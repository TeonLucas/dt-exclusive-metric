package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	Interval            = 3600000
	DefaultPollInterval = "5m"
	ServiceEndpoint     = "https://distributed-tracing.service.newrelic.com/api/v2/analytics/relatedTraceEntitySummaries"
)

// To parse JSON from APIs
type EntitySummaries struct {
	Data struct {
		ByEntity []Entity `json:"byEntity"`
	} `json:"data"`
}
type Entity struct {
	Direction         string  `json:"direction"`
	Depth             float32 `json:"depth"`
	CallPath          string  `json:"entityCallPath"`
	Guid              string  `json:"entityGuid"`
	Count             int     `json:"count"`
	ErrorCount        int     `json:"errorCount"`
	Duration          float32 `json:"averageDurationMs"`
	ExclusiveDuration float32 `json:"averageExclusiveDurationMs"`
}

// To store and analyze data
type AccountData struct {
	AccountId      string
	Guid           string
	GuidMap        map[string]string
	LicenseKey     string
	UserKey        string
	CDPctx         context.Context
	CDPcancel      context.CancelFunc
	Client         *http.Client
	GraphQlHeaders []string
	MetricHeaders  []string
	Details        Details
	Response       []Entity
	SampleTime     int64
	PollInterval   time.Duration
}
type Details struct {
	EntityGuid  string `json:"entityGuid"`
	CurrentTime int64  `json:"-"`
	StartTime   int64  `json:"startTimeMs"`
	Duration    int    `json:"durationMs"`
}

func main() {
	var err error

	// Get required settings
	data := AccountData{
		AccountId:  os.Getenv("NEW_RELIC_ACCOUNT"),
		Guid:       os.Getenv("ENTITY_GUID"),
		LicenseKey: os.Getenv("NEW_RELIC_LICENSE_KEY"),
		UserKey:    os.Getenv("NEW_RELIC_USER_KEY"),
	}

	if len(data.AccountId) == 0 {
		log.Printf("Please set env var NEW_RELIC_ACCOUNT")
		os.Exit(0)
	}
	if len(data.Guid) == 0 {
		log.Printf("Please set env var ENTITY_GUID")
		os.Exit(0)
	}
	if len(data.LicenseKey) == 0 {
		log.Printf("Please set env var NEW_RELIC_LICENSE_KEY")
		os.Exit(0)
	}
	if len(data.UserKey) == 0 {
		log.Printf("Please set env var NEW_RELIC_USER_KEY")
		os.Exit(0)
	}
	log.Printf("Using account %s, entity Guid %s", data.AccountId, data.Guid)

	// Get poll interval
	pollInterval := os.Getenv("POLL_INTERVAL")
	if len(pollInterval) == 0 {
		pollInterval = DefaultPollInterval
	}
	data.PollInterval, err = time.ParseDuration(pollInterval)
	if err != nil {
		log.Fatalf("Error: could not parse env var POLL_INTERVAL: %s, must be a duration (ex: 1h)", err)
	}
	log.Printf("Poll interval is %s", data.PollInterval)

	// Open Chrome to scrape data from a NR1 login
	data.startChromeAndLogin()

	// Create GraphQl client
	data.makeClient()

	// Graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sigs := <-sigs
		log.Printf("Process %v - Shutting down\n", sigs)
		os.Exit(0)
	}()

	// Start poll loop
	log.Println("Starting polling loop")
	for {
		startTime := time.Now()
		data.SampleTime = startTime.Unix()

		// Call EntitySummaries endpoint
		data.postServiceEndpoint()

		// Make results into metrics
		data.makeMetrics()

		remainder := data.PollInterval - time.Now().Sub(startTime)
		if remainder > 0 {
			log.Printf("Sleeping %v", remainder)

			// Wait remainder of poll interval
			time.Sleep(remainder)
		}
	}
}
