package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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
	// 1. Setup Configuration
	targetDate := "13-05-2026"
	origin := "SXR"
	dest := "BLR"
	
	// Format date for the API URL (removes dashes)
	urlDate := strings.ReplaceAll(targetDate, "-", "")

	// 2. Fetch Data from Ixigo
	url := fmt.Sprintf(
		"https://www.ixigo.com/outlook/v1/onward/ranged?departureDate=%s&destination=%s&fareClass=e&origin=%s&paxCombinationType=100&refundTypes=REFUNDABLE%%2CNON_REFUNDABLE%%2CPARTIALLY_REFUNDABLE",
		urlDate, dest, origin,
	)
	
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("apikey", "ixiweb!2$")
	req.Header.Set("clientid", "ixiweb")
	req.Header.Set("user-agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Ixigo API Error", 500)
		return
	}
	defer resp.Body.Close()

	var rawResponse IxigoResponse
	if err := json.NewDecoder(resp.Body).Decode(&rawResponse); err != nil {
		http.Error(w, "JSON Error", 500)
		return
	}

	// 3. Filter for ONLY the target date
	var flightResults []IxigoResult
	for _, f := range rawResponse.Data.Going.Results {
		if f.Date == targetDate {
			flightResults = append(flightResults, f)
		}
	}

	// 4. Redis State Management & Trend Detection
	redisURL := os.Getenv("REDIS_URL")
	opts, _ := redis.ParseURL(redisURL)
	rdb := redis.NewClient(opts)
	defer rdb.Close()

	// Unique key for this specific route and date
	stateKey := fmt.Sprintf("flights:%s:%s:%s", origin, dest, targetDate)

	// Map of FlightNumber -> Fare for comparison
	currentPrices := make(map[string]float64)
	for _, f := range flightResults {
		currentPrices[f.FlightNumber] = f.Fare
	}
	newJSON, _ := json.Marshal(currentPrices)

	// Fetch previous state
	oldJSON, _ := rdb.Get(ctx, stateKey).Result()
	oldPrices := make(map[string]float64)
	json.Unmarshal([]byte(oldJSON), &oldPrices)

	hasChanged := false
	trends := make(map[string]string) 

	for _, f := range flightResults {
		oldFare, exists := oldPrices[f.FlightNumber]
		if !exists {
			trends[f.FlightNumber] = "🆕"
			hasChanged = true
		} else if f.Fare < oldFare {
			trends[f.FlightNumber] = "🟢" // Price dropped
			hasChanged = true
		} else if f.Fare > oldFare {
			trends[f.FlightNumber] = "🔴" // Price rose
			hasChanged = true
		} else {
			trends[f.FlightNumber] = "⚪" // No change
		}
	}

	// 5. Notify if changes detected
	if hasChanged && len(flightResults) > 0 {
		rdb.Set(ctx, stateKey, newJSON, 0)
		sendToDiscord(targetDate, origin, dest, flightResults, trends)
		w.Write([]byte(`{"status":"success","action":"notified"}`))
	} else {
		w.Write([]byte(`{"status":"success","action":"skipped_no_change"}`))
	}
}

func sendToDiscord(date, origin, dest string, flights []IxigoResult, trends map[string]string) {
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL_SXR_BLR")
	if webhookURL == "" { return }

	var fields []map[string]interface{}
	for _, f := range flights {
		airline := f.Airline
		if airline == "" { airline = f.AirlineCode }
		
		fields = append(fields, map[string]interface{}{
			"name":   fmt.Sprintf("%s %s (%s)", trends[f.FlightNumber], airline, f.FlightNumber),
			"value":  fmt.Sprintf("💰 **₹%.0f**", f.Fare),
			"inline": true,
		})
	}

	payload := map[string]interface{}{
		"embeds": []interface{}{
			map[string]interface{}{
				"title":       fmt.Sprintf("✈️ Price Update: %s ➔ %s", origin, dest),
				"description": fmt.Sprintf("Monitoring Date: **%s**\n🟢 Down | 🔴 Up | 🆕 New | ⚪ Flat", date),
				"color":       3066993, 
				"fields":      fields,
				"footer":      map[string]string{"text": "Ixigo Flight Tracker"},
			},
		},
	}

	body, _ := json.Marshal(payload)
	http.Post(webhookURL, "application/json", bytes.NewBuffer(body))
}
