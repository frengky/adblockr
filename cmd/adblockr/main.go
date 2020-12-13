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
	dbFlag      = "adblockr.db"
	verbose     = false

	rootCmd = &cobra.Command{
		Use:   "adblockr",
		Short: "High performance DNS proxy with ad filter",
		Long:  "High performance DNS proxy with ad filter written in Go",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {

		},
	}

	serveCmd = &cobra.Command{
		Use:   "serve",
		Short: "Start DNS proxy server",
		Long:  "Start DNS proxy server",
		Run: func(cmd *cobra.Command, args []string) {
			runServe()
		},
	}

	initDbCmd = &cobra.Command{
		Use:   "init-db",
		Short: "Initialize blacklist database file",
		Long:  "Initialize blacklist database file",
		Run: func(cmd *cobra.Command, args []string) {
			runInitDb()
		},
	}
)

func init() {
	cobra.OnInitialize(initConfig)

	serveCmd.Flags().IntVarP(&intervalMs, "interval", "i", intervalMs, "DNS resolver interval (ms)")
	serveCmd.Flags().IntVarP(&timeoutSecs, "timeout", "t", timeoutSecs, "DNS resolver timeout (seconds)")

	initDbCmd.Flags().StringVarP(&dbFlag, "file", "f", dbFlag, "Path to database file")

	rootCmd.PersistentFlags().StringVarP(&configFlag, "config", "c", configFlag, "Path to configuration file")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", verbose, "Verbose output")
	rootCmd.AddCommand(serveCmd, initDbCmd)
}

func initConfig() {
	if verbose {
		log.SetLevel(log.DebugLevel)
	}

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
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func initDomainBucket(sourceUri []string, store adblockr.DomainBucket) {
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

func runInitDb() {
	logCtx := log.WithField("file", dbFlag)
	if fileExists(dbFlag) {
		logCtx.Error("file already exists, aborting")
		os.Exit(1)
	}

	blacklist := adblockr.NewDbDomainBucket().(*adblockr.DbDomainBucket)
	if err := blacklist.Open(dbFlag); err != nil {
		logCtx.WithError(err).Error("error opening database")
		os.Exit(1)
	}
	defer blacklist.Close()
	initDomainBucket(config.Blacklist, blacklist)
}

func runServe() {
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
		initDomainBucket(config.Blacklist, blacklist)
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
