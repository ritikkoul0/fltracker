package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"
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

func Handler(w http.ResponseWriter, r *http.Request) {
	// 1. Setup Request
	targetDate := "10-05-2026"
	url := "https://www.ixigo.com/outlook/v1/onward/ranged?departureDate=10052026&destination=BLR&fareClass=e&origin=SXR&paxCombinationType=100&refundTypes=REFUNDABLE%2CNON_REFUNDABLE%2CPARTIALLY_REFUNDABLE"

	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)

	// Required Headers from your curl
	req.Header.Set("accept", "*/*")
	req.Header.Set("apikey", "ixiweb!2$")
	req.Header.Set("clientid", "ixiweb")
	req.Header.Set("ixisrc", "ixiweb")
	req.Header.Set("uuid", "d07889cb18b346a0ac58")
	req.Header.Set("deviceid", "d07889cb18b346a0ac58")
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36")

	// 2. Hit API
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Failed to reach Ixigo", 500)
		return
	}
	defer resp.Body.Close()

	var rawResponse IxigoResponse
	if err := json.NewDecoder(resp.Body).Decode(&rawResponse); err != nil {
		http.Error(w, "Failed to decode JSON", 500)
		return
	}

	// 3. Filter: Skip if Airline, Code, or FlightNumber is empty
	var filtered []IxigoResult
	for _, f := range rawResponse.Data.Going.Results {
		// Rule: Only target date AND skip empty identifiers
		if f.Date == targetDate && f.Airline != "" && f.AirlineCode != "" && f.FlightNumber != "" && f.Fare > 0 {
			filtered = append(filtered, f)
		}
	}

	// 4. Sort: Find cheapest fares first
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Fare < filtered[j].Fare
	})

	// 5. Select first 5
	limit := 5
	if len(filtered) < 5 {
		limit = len(filtered)
	}
	finalSelection := filtered[:limit]

	// 6. Send to Discord
	if len(finalSelection) > 0 {
		sendToDiscord(targetDate, finalSelection)
	}

	// Response summary
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"success","date":"%s","valid_flights_found":%d}`, targetDate, len(finalSelection))
}

func sendToDiscord(date string, flights []IxigoResult) {
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" {
		return
	}

	var fields []map[string]interface{}
	for i, f := range flights {
		fields = append(fields, map[string]interface{}{
			"name":   fmt.Sprintf("%d. %s (%s)", i+1, f.Airline, f.AirlineCode),
			"value":  fmt.Sprintf("💰 Fare: **₹%.0f**\n🔢 Flight: `%s`", f.Fare, f.FlightNumber),
			"inline": false,
		})
	}

	payload := map[string]interface{}{
		"embeds": []interface{}{
			map[string]interface{}{
				"title":       fmt.Sprintf("✈️ 5 Cheapest Verified Flights: %s", date),
				"description": "SXR ➔ BLR (Excluding empty placeholders)",
				"color":       3066993,
				"fields":      fields,
				"footer":      map[string]interface{}{"text": "Monitor Alert • " + time.Now().Format("15:04")},
			},
		},
	}

	body, _ := json.Marshal(payload)
	http.Post(webhookURL, "application/json", bytes.NewBuffer(body))
}
