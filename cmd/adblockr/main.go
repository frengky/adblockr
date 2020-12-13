package main

import (
	"github.com/frengky/adblockr"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

type ServerConfig struct {
	ListenAddress string   `yaml:"listen_address"`
	Nameservers   []string `yaml:"nameservers,flow"`
	Blacklist     []string `yaml:"blacklist,flow"`
	DbFile        string   `yaml:"db_file"`
}

var (
	config      = &ServerConfig{}
	configFlag  = "adblockr.yml"
	intervalMs  = 800
	timeoutSecs = 5
	createDb    string

	rootCmd = &cobra.Command{
		Use:   "adblockr",
		Short: "DNS proxy with ad filter",
		Long:  "DNS proxy with ad filter written in Go",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			logCtx := log.WithField("config", configFlag)

			f, err := os.Open(configFlag)
			if err != nil {
				logCtx.WithError(err).Error("unable to open configuration file")
				os.Exit(1)
			}
			defer f.Close()

			decoder := yaml.NewDecoder(f)
			err = decoder.Decode(config)
			if err != nil {
				logCtx.WithError(err).Error("invalid configuration file format")
				os.Exit(1)
			}
		},
		Run: func(cmd *cobra.Command, args []string) {
			if createDb != "" {
				runCreateDb()
			} else {
				runServer()
			}
		},
	}
)

func init() {
	log.SetLevel(log.DebugLevel)

	rootCmd.PersistentFlags().StringVarP(&configFlag, "config", "c", "adblockr.yml", "Path to configuration file")
	rootCmd.MarkPersistentFlagRequired("config")
	rootCmd.PersistentFlags().IntVarP(&intervalMs, "interval", "i", intervalMs, "DNS resolver interval (ms)")
	rootCmd.PersistentFlags().IntVarP(&timeoutSecs, "timeout", "t", timeoutSecs, "DNS resolver timeout (seconds)")
	rootCmd.PersistentFlags().StringVar(&createDb, "create-db", "", "Initialize blacklist database file (path)")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func initDomainStore(sourceUri []string, store adblockr.DomainBucket) {
	log.Info("initializing blacklist data, may take a while...")

	total := 0
	source := 0

	for _, uri := range sourceUri {
		source++
		log.Info("processing ", uri)
		count, err := store.Update(uri)
		if err != nil {
			log.WithError(err).Errorf("download failed")
			continue
		}
		log.WithField("count", count).Info("download success")
		total = total + count
	}

	log.WithFields(log.Fields{"total": total, "source": source}).Info("blacklist data initialized")
}

func runCreateDb() {
	logCtx := log.WithField("file", createDb)
	if fileExists(createDb) {
		logCtx.Error("file already exists, aborting")
		os.Exit(1)
	}

	blacklist := adblockr.NewDbDomainBucket().(*adblockr.DbDomainBucket)
	if err := blacklist.Open(createDb); err != nil {
		logCtx.WithError(err).Error("error opening database")
		os.Exit(1)
	}
	defer blacklist.Close()
	initDomainStore(config.Blacklist, blacklist)
}

func runServer() {
	var blacklist adblockr.DomainBucket
	var init = false

	if config.DbFile == "" {
		log.Info("starting DNS proxy with ad filter (using in-memory backend)")
		init = true
		blacklist = adblockr.NewMemDomainBucket()
	} else {
		log.WithField("file", config.DbFile).Info("starting DNS proxy with ad filter (using db backend)")
		init = !fileExists(config.DbFile)
		blacklist = adblockr.NewDbDomainBucket()
		if err := blacklist.(*adblockr.DbDomainBucket).Open(config.DbFile); err != nil {
			log.WithField("file", config.DbFile).WithError(err).Error("unable to open database")
			os.Exit(1)
		}
		defer blacklist.(*adblockr.DbDomainBucket).Close()
	}
	if init {
		initDomainStore(config.Blacklist, blacklist)
	}

	var wg sync.WaitGroup

	sigChan := make(chan os.Signal)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGKILL)

	resolver := adblockr.NewResolver(config.Nameservers, intervalMs, timeoutSecs)
	server := adblockr.NewServer(config.ListenAddress, resolver, blacklist)

	wg.Add(1)
	go func() {
		defer wg.Done()
		server.ListenAndServe()
	}()

	<-sigChan
	server.Shutdown()
	wg.Wait()
	os.Exit(0)
}

func fileExists(filepath string) bool {
	info, err := os.Stat(filepath)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}
