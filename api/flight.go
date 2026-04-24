package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// --- Models ---
type IxigoResult struct {
	Airline      string  `json:"airline"`
	AirlineCode  string  `json:"airlineCode"`
	FlightNumber string  `json:"flightNumber"`
	Date         string  `json:"date"`
	Fare         float64 `json:"fare"`
}

type IxigoResponse struct {
	Data struct {
		Going struct {
			Results []IxigoResult `json:"results"`
		} `json:"going"`
	} `json:"data"`
}

var ctx = context.Background()

func Handler(w http.ResponseWriter, r *http.Request) {
	const layout = "02-01-2006"
	targetStr := "10-05-2026"
	urlDate := strings.ReplaceAll(targetStr, "-", "")
	centerDate, _ := time.Parse(layout, targetStr)

	// 1. Generate the 7-day window (07-05 to 13-05)
	allowedDates := make(map[string]bool)
	for i := -3; i <= 3; i++ {
		d := centerDate.AddDate(0, 0, i)
		allowedDates[d.Format(layout)] = true
	}

	// 2. Fetch Data
	url := fmt.Sprintf("https://www.ixigo.com/outlook/v1/onward/ranged?departureDate=%s&destination=BLR&fareClass=e&origin=SXR&paxCombinationType=100&refundTypes=REFUNDABLE%%2CNON_REFUNDABLE%%2CPARTIALLY_REFUNDABLE", urlDate)
	
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("apikey", "ixiweb!2$")
	req.Header.Set("clientid", "ixiweb")
	req.Header.Set("ixisrc", "ixiweb")
	req.Header.Set("user-agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "API Failed", 500)
		return
	}
	defer resp.Body.Close()

	var rawResponse IxigoResponse
	if err := json.NewDecoder(resp.Body).Decode(&rawResponse); err != nil {
		http.Error(w, "Decode Failed", 500)
		return
	}

	// 3. Filter and Sort Chronologically
	var windowFlights []IxigoResult
	for _, f := range rawResponse.Data.Going.Results {
		if allowedDates[f.Date] {
			windowFlights = append(windowFlights, f)
		}
	}

	sort.Slice(windowFlights, func(i, j int) bool {
		t1, _ := time.Parse(layout, windowFlights[i].Date)
		t2, _ := time.Parse(layout, windowFlights[j].Date)
		return t1.Before(t2)
	})

	// 4. Change Detection via Redis Fingerprint
	var currentFares []string
	for _, f := range windowFlights {
		currentFares = append(currentFares, fmt.Sprintf("%.0f", f.Fare))
	}
	newFingerprint := strings.Join(currentFares, "|")

	// Connect to Redis
	kvURL := os.Getenv("KV_URL")
	opts, err := redis.ParseURL(kvURL)
	if err != nil {
		http.Error(w, "Redis Config Error", 500)
		return
	}
	rdb := redis.NewClient(opts)

	// Check against previous state
	oldFingerprint, _ := rdb.Get(ctx, "flight_window_state").Result()

	if newFingerprint != oldFingerprint && len(windowFlights) > 0 {
		// Update Redis and Notify Discord
		rdb.Set(ctx, "flight_window_state", newFingerprint, 0)
		sendToDiscord(targetStr, windowFlights)
		
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"success","action":"notified"}`)
	} else {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"success","action":"skipped_no_change"}`)
	}
}

func sendToDiscord(centerDate string, flights []IxigoResult) {
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" { return }

	var fields []map[string]interface{}
	for _, f := range flights {
		airline := f.Airline
		if airline == "" { airline = "Details Pending" }
		
		fields = append(fields, map[string]interface{}{
			"name":   fmt.Sprintf("📅 %s", f.Date),
			"value":  fmt.Sprintf("💰 **₹%.0f**\n✈️ %s", f.Fare, airline),
			"inline": true,
		})
	}

	payload := map[string]interface{}{
		"embeds": []interface{}{
			map[string]interface{}{
				"title":       "🔔 Price Change Alert: ±3 Day Window",
				"description": fmt.Sprintf("Fares updated for the week of %s", centerDate),
				"color":       15105570, // Orange
				"fields":      fields,
				"footer":      map[string]interface{}{"text": "Flight Monitor • " + time.Now().Format("15:04")},
			},
		},
	}

	body, _ := json.Marshal(payload)
	http.Post(webhookURL, "application/json", bytes.NewBuffer(body))
}
