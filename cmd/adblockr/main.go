package main

import (
	"fmt"
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
	config          = &ServerConfig{}
	configFlag      = "adblockr.yml"
	intervalMs      = 500
	timeoutSecs     = 2
	dbFlag          = "adblockr.db"
	verbose         = false
	parseSourceFlag string

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
		Short: "Initialize domain blacklist database file",
		Long:  "Initialize domain blacklist database file",
		Run: func(cmd *cobra.Command, args []string) {
			runInitDb()
		},
	}

	parseCmd = &cobra.Command{
		Use:   "parse",
		Short: "Parse a compatible host file format to domain list",
		Long:  "Parse a compatible host file format to domain list",
		Run: func(cmd *cobra.Command, args []string) {
			runParse()
		},
	}
)

func init() {
	cobra.OnInitialize(initConfig)

	serveCmd.Flags().IntVarP(&intervalMs, "interval", "i", intervalMs, "Nameserver switch interval (ms)")
	serveCmd.Flags().IntVarP(&timeoutSecs, "timeout", "t", timeoutSecs, "Nameserver resolve timeout (seconds)")

	initDbCmd.Flags().StringVarP(&dbFlag, "file", "f", dbFlag, "Path to database file")

	parseCmd.Flags().StringVarP(&parseSourceFlag, "source", "s", parseSourceFlag,
		"Blacklist source URI, \"file///path/to.txt\" or \"http://some.where/blacklist.txt\"")
	parseCmd.MarkFlagRequired("source")

	rootCmd.PersistentFlags().StringVarP(&configFlag, "config", "c", configFlag, "Path to configuration file")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", verbose, "Verbose output")
	rootCmd.AddCommand(serveCmd, initDbCmd, parseCmd)
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

func initBlacklistFromSources(sourceUri []string, store adblockr.DomainBucket) {
	log.Info("initializing blacklist database, may take a while...")

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

	log.WithFields(log.Fields{"total": total, "source": source}).Info("blacklist database initialized")
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
	initBlacklistFromSources(config.Blacklist, blacklist)
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
		initBlacklistFromSources(config.Blacklist, blacklist)
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

func runParse() {
	if parseSourceFlag == "" {
		log.Error("No file specified")
		os.Exit(1)
	}

	logCtx := log.WithField("uri", parseSourceFlag)

	r, err := adblockr.OpenResource(parseSourceFlag)
	if err != nil {
		logCtx.WithError(err).Error("unable to open uri")
		os.Exit(1)
	}
	defer r.Close()

	fmt.Fprintln(os.Stdout, fmt.Sprintf("# %s", parseSourceFlag))
	count, err := adblockr.ParseLine(r, func(line string) bool {
		fmt.Fprintln(os.Stdout, line)
		return true
	})
	fmt.Fprintln(os.Stdout, fmt.Sprintf("# Total %d", count))

	if err != nil {
		logCtx.WithError(err).Error("error while reading file")
		os.Exit(1)
	}

	os.Exit(0)
}

func fileExists(filepath string) bool {
	info, err := os.Stat(filepath)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}
