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

	// 1. Generate the 7-day window
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
	req.Header.Set("user-agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "API Failed", 500)
		return
	}
	defer resp.Body.Close()

	var rawResponse IxigoResponse
	json.NewDecoder(resp.Body).Decode(&rawResponse)

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

	// 4. Change Detection Logic (Always proceeds, but determines status)
	var currentFingerprint []string
	for _, f := range windowFlights {
		currentFingerprint = append(currentFingerprint, fmt.Sprintf("%.0f", f.Fare))
	}
	newFingerprint := strings.Join(currentFingerprint, "|")

	opts, _ := redis.ParseURL(os.Getenv("KV_URL"))
	rdb := redis.NewClient(opts)

	oldFingerprint, _ := rdb.Get(ctx, "flight_price_fingerprint").Result()
	
	status := "STABLE"
	color := 3447003 // Blue
	
	if newFingerprint != oldFingerprint {
		status = "PRICE CHANGE DETECTED"
		color = 15105570 // Orange
		rdb.Set(ctx, "flight_price_fingerprint", newFingerprint, 0)
	}

	// 5. Send notification EVERY time
	if len(windowFlights) > 0 {
		sendToDiscord(targetStr, windowFlights, status, color)
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"notified","price_status":"%s"}`, status)
}

func sendToDiscord(centerDate string, flights []IxigoResult, status string, color int) {
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" {
		return
	}

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
				"title":       fmt.Sprintf("✈️ %s: %s (±3 Days)", status, centerDate),
				"description": "Showing current fares for SXR ➔ BLR",
				"color":       color,
				"fields":      fields,
				"footer":      map[string]interface{}{"text": "Daily Check-in • " + time.Now().Format("15:04")},
			},
		},
	}

	body, _ := json.Marshal(payload)
	http.Post(webhookURL, "application/json", bytes.NewBuffer(body))
}
