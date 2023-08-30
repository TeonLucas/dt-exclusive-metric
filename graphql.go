package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	GraphQlEndpoint = "https://api.newrelic.com/graphql"
	GraphQlQuery    = "{actor {entities(guids: [%s]) {name guid type}}}"
	MetricEndoint   = "https://metric-api.newrelic.com/metric/v1"
)

type GraphQlPayload struct {
	Query string `json:"query"`
}

type GraphQlResult struct {
	Data struct {
		Actor struct {
			Entities []struct {
				Name string `json:"name"`
				Guid string `json:"guid"`
			} `json:"entities"`
		} `json:"actor"`
	} `json:"data"`
}

type MetricPayload struct {
	Metrics []Metric `json:"metrics"`
}

type Metric struct {
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Value      float32    `json:"value"`
	Timestamp  int64      `json:"timestamp"`
	Attributes Attributes `json:"attributes"`
}
type Attributes struct {
	Name  string `json:"name"`
	Depth int    `json:"depth"`
	Guid  string `json:"entity.guid"`
}

// Make API request with error retry
func retryQuery(client *http.Client, method, url, data string, headers []string) (b []byte) {
	var res *http.Response
	var err error
	var body io.Reader

	if len(data) > 0 {
		body = strings.NewReader(data)
	}

	// up to 3 retries on API error
	for j := 1; j <= 3; j++ {
		req, _ := http.NewRequest(method, url, body)
		for _, h := range headers {
			params := strings.Split(h, ":")
			req.Header.Set(params[0], params[1])
		}
		res, err = client.Do(req)
		if err != nil {
			log.Println(err)
		}
		if res != nil {
			if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusAccepted {
				break
			}
			log.Printf("Retry %d: http status %d", j, res.StatusCode)
		} else {
			log.Printf("Retry %d: no response", j)
		}
		time.Sleep(500 * time.Millisecond)
	}
	b, err = io.ReadAll(res.Body)
	if err != nil {
		log.Println(err)
		return
	}
	res.Body.Close()
	return
}

func (data *AccountData) makeClient() {
	data.Client = &http.Client{}
	data.GraphQlHeaders = []string{"Content-Type:application/json", "API-Key:" + data.UserKey}
	data.MetricHeaders = []string{"Content-Type:application/json", "Api-Key:" + data.LicenseKey}
	data.GuidMap = make(map[string]string)
}

func (data *AccountData) makeMetrics() {
	var err error
	var ok bool
	var guids []string
	var gQuery GraphQlPayload
	var j []byte
	var metrics []Metric
	metricMap := make(map[string]Metric)

	for _, entity := range data.Response {
		_, ok = metricMap[entity.Guid]

		// only insert each guid once
		if !ok {
			name, named := data.GuidMap[entity.Guid]
			if !named {
				name = entity.Guid
				guids = append(guids, fmt.Sprintf("%q", entity.Guid))
			}
			metricMap[entity.Guid] = Metric{
				Name:      "exclusiveDuration",
				Type:      "gauge",
				Value:     entity.ExclusiveDuration,
				Timestamp: data.Details.CurrentTime,
				Attributes: Attributes{
					Name:  name,
					Depth: int(entity.Depth),
					Guid:  entity.Guid,
				},
			}
		}
	}

	// Make graphQl request to lookup entity names by guid (if not already cached)
	if len(guids) > 0 {
		guidList := strings.Join(guids, ",")
		gQuery.Query = fmt.Sprintf(GraphQlQuery, guidList)
		j, err = json.Marshal(gQuery)
		if err != nil {
			log.Printf("Error creating GraphQl query: %v", err)
		}

		log.Printf("Looking up names for %s", guidList)
		b := retryQuery(data.Client, "POST", GraphQlEndpoint, string(j), data.GraphQlHeaders)

		// parse results
		var graphQlResult GraphQlResult
		var metric Metric
		log.Printf("Parsing response %d bytes", len(b))
		err = json.Unmarshal(b, &graphQlResult)
		if err != nil {
			log.Printf("Error parsing GraphQl result: %v", err)
		}

		for _, result := range graphQlResult.Data.Actor.Entities {
			// cache for later
			data.GuidMap[result.Guid] = result.Name
			// update metrics with entity names
			metric, ok = metricMap[result.Guid]
			if ok {
				metric.Attributes.Name = result.Name
				metricMap[result.Guid] = metric
			}
		}
	}

	// send array of metrics to api
	for _, metric := range metricMap {
		metrics = append(metrics, metric)
	}
	if len(metrics) == 0 {
		log.Println("No metrics to send")
	} else {
		j, err = json.Marshal([]MetricPayload{{Metrics: metrics}})
		if err != nil {
			log.Printf("Error creating Metric payload: %v", err)
		}
		log.Printf("Sending %d metrics to the metric api", len(metrics))
		b := retryQuery(data.Client, "POST", MetricEndoint, string(j), data.MetricHeaders)
		log.Printf("Submitted %s", b)
	}
}
