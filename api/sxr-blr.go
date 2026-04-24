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
	targetStr := "15-05-2026"
	
	// Change these per file
	origin := "SXR"
	dest := "BLR"

	urlDate := strings.ReplaceAll(targetStr, "-", "")
	centerDate, _ := time.Parse(layout, targetStr)

	// 1. Setup Window (±3 days)
	allowedDates := make(map[string]bool)
	for i := -3; i <= 3; i++ {
		d := centerDate.AddDate(0, 0, i)
		allowedDates[d.Format(layout)] = true
	}

	// 2. Fetch Data
	url := fmt.Sprintf(
		"https://www.ixigo.com/outlook/v1/onward/ranged?departureDate=%s&destination=%s&fareClass=e&origin=%s&paxCombinationType=100&refundTypes=REFUNDABLE%%2CNON_REFUNDABLE%%2CPARTIALLY_REFUNDABLE",
		urlDate,
		dest,
		origin,
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

	var windowFlights []IxigoResult
	for _, f := range rawResponse.Data.Going.Results {
		if allowedDates[f.Date] {
			windowFlights = append(windowFlights, f)
		}
	}

	// 3. Sort Chronologically
	sort.Slice(windowFlights, func(i, j int) bool {
		t1, _ := time.Parse(layout, windowFlights[i].Date)
		t2, _ := time.Parse(layout, windowFlights[j].Date)
		return t1.Before(t2)
	})

	// 4. Redis Logic
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		http.Error(w, "Missing REDIS_URL", 500)
		return
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		http.Error(w, "Invalid REDIS_URL", 500)
		return
	}

	rdb := redis.NewClient(opts)
	defer rdb.Close()

	// CREATE A UNIQUE KEY FOR THIS SPECIFIC ROUTE
	// This prevents File A (BLR-SXR) from overwriting File B (DEL-BOM)
	stateKey := fmt.Sprintf("flights:%s:%s", origin, dest)

	// Create Price Fingerprint
	var currentFares []string
	for _, f := range windowFlights {
		currentFares = append(currentFares, fmt.Sprintf("%.0f", f.Fare))
	}
	newFingerprint := strings.Join(currentFares, "|")

	// Compare State using the dynamic key
	oldFingerprint, _ := rdb.Get(ctx, stateKey).Result()

	if newFingerprint != oldFingerprint && len(windowFlights) > 0 {
		rdb.Set(ctx, stateKey, newFingerprint, 0)
		sendToDiscord(targetStr, origin, dest, windowFlights)
		w.Write([]byte(`{"status":"success","action":"notified"}`))
	} else {
		w.Write([]byte(`{"status":"success","action":"skipped_no_change"}`))
	}
}

func sendToDiscord(centerDate, origin, dest string, flights []IxigoResult) {
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL_SXR_BLR")
	if webhookURL == "" { return }

	var fields []map[string]interface{}
	for _, f := range flights {
		airline := f.Airline
		if airline == "" { airline = "Pending" }
		
		fields = append(fields, map[string]interface{}{
			"name":   fmt.Sprintf("📅 %s", f.Date),
			"value":  fmt.Sprintf("💰 **₹%.0f**\n✈️ %s", f.Fare, airline),
			"inline": true,
		})
	}

	payload := map[string]interface{}{
		"embeds": []interface{}{
			map[string]interface{}{
				"title":       fmt.Sprintf("✈️ Price Update: %s ➔ %s", origin, dest),
				"description": fmt.Sprintf("Price changes detected for travel around %s", centerDate),
				"color":       3066993,
				"fields":      fields,
			},
		},
	}

	body, _ := json.Marshal(payload)
	http.Post(webhookURL, "application/json", bytes.NewBuffer(body))
}
