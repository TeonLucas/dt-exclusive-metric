package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/chromedp/cdproto/runtime"
	"log"
	"time"

	"github.com/chromedp/chromedp"
)

// For Chrome web driver
func overrideHeadless() []chromedp.ExecAllocatorOption {
	return []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.DisableGPU,

		// After Puppeteer's default behavior.
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("enable-features", "NetworkService,NetworkServiceInProcess"),
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("disable-backgrounding-occluded-windows", true),
		chromedp.Flag("disable-breakpad", true),
		chromedp.Flag("disable-client-side-phishing-detection", true),
		chromedp.Flag("disable-default-apps", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-features", "site-per-process,TranslateUI,BlinkGenPropertyTrees"),
		chromedp.Flag("disable-hang-monitor", true),
		chromedp.Flag("disable-ipc-flooding-protection", true),
		chromedp.Flag("disable-popup-blocking", true),
		chromedp.Flag("disable-prompt-on-repost", true),
		chromedp.Flag("disable-renderer-backgrounding", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("force-color-profile", "srgb"),
		chromedp.Flag("metrics-recording-only", true),
		chromedp.Flag("safebrowsing-disable-auto-update", true),
		chromedp.Flag("enable-automation", true),
		chromedp.Flag("password-store", "basic"),
		chromedp.Flag("use-mock-keychain", true),
	}
}

func login(accountId string) chromedp.Tasks {
	return chromedp.Tasks{
		// Navigate to NR ui
		chromedp.Navigate("https://login.newrelic.com/login"),

		// Wait for login complete
		chromedp.WaitNotPresent("form#login"),

		// Ask for user input
		chromedp.ActionFunc(func(ctx context.Context) error {
			fmt.Println("New Relic login page, please log in")
			time.Sleep(5 * time.Second)
			return nil
		}),

		// Wait for login complete
		chromedp.WaitVisible("body.wanda"),

		// Ask for user input
		chromedp.ActionFunc(func(ctx context.Context) error {
			fmt.Println("Login complete")
			time.Sleep(2 * time.Second)
			return nil
		}),
		chromedp.Navigate(fmt.Sprintf("https://one.newrelic.com/nr1-core?account=%s", accountId)),
	}
}

func (data *AccountData) startChromeAndLogin() {
	// Launch scraper
	var err error
	fmt.Println("Launching Chrome web scraper")
	opts := overrideHeadless()
	ctx, _ := chromedp.NewExecAllocator(context.Background(), opts...)
	data.CDPctx, data.CDPcancel = chromedp.NewContext(ctx, chromedp.WithLogf(log.Printf))

	// Do login
	if err = chromedp.Run(data.CDPctx, login(data.AccountId)); err != nil {
		fmt.Println("Login error:", err)
	}
}

func postEntitySummaries(data *AccountData) chromedp.Tasks {
	pl, _ := json.Marshal(data.Details)
	fn := "async function fetchSummaries(data = {}) {const r = await fetch('" + ServiceEndpoint + "', {method: 'POST', headers: {'Content-Type': 'application/json; charset=utf-8', 'Accept': 'application/json'}, credentials: 'include', body: JSON.stringify(data)}); return r.json()};"
	ex := fmt.Sprintf(" fetchSummaries(%s)", pl)
	var res EntitySummaries

	//log.Println("Defining: " + fn)
	//log.Println("Calling:" + ex)

	return chromedp.Tasks{
		// Post to DT service via NR ui
		chromedp.ActionFunc(func(ctx context.Context) error {
			log.Printf("Posting to %s", ServiceEndpoint)
			time.Sleep(time.Second)
			return nil
		}),
		chromedp.Evaluate(fn+ex, &res, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			time.Sleep(2 * time.Second)
			log.Printf("Got response with %d trace entities", len(res.Data.ByEntity))
			data.Response = res.Data.ByEntity
			return nil
		}),
	}
}

func (data *AccountData) postServiceEndpoint() {
	// Scrape summary timings for entity
	currentTime := time.Now().UnixMilli()
	data.Details = Details{
		EntityGuid:  data.Guid,
		CurrentTime: currentTime,
		StartTime:   currentTime - Interval,
		Duration:    Interval,
	}
	var err error

	// Do postEntitySummaries
	if err = chromedp.Run(data.CDPctx, postEntitySummaries(data)); err != nil {
		log.Println(err)
	}
}
