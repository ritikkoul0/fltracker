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
	targetDate := "09-05-2026"
	// Updated URL with your exact parameters
	url := "https://www.ixigo.com/outlook/v1/onward/ranged?departureDate=09052026&destination=BLR&fareClass=e&origin=SXR&paxCombinationType=100&refundTypes=REFUNDABLE%2CNON_REFUNDABLE%2CPARTIALLY_REFUNDABLE"

	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)

	// Set Headers exactly as per your curl request
	req.Header.Set("accept", "*/*")
	req.Header.Set("apikey", "ixiweb!2$")
	req.Header.Set("clientid", "ixiweb")
	req.Header.Set("ixisrc", "ixiweb") // Added this from your curl
	req.Header.Set("uuid", "d07889cb18b346a0ac58")
	req.Header.Set("deviceid", "d07889cb18b346a0ac58")
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "API Connection Failed", 500)
		return
	}
	defer resp.Body.Close()

	var rawResponse IxigoResponse
	if err := json.NewDecoder(resp.Body).Decode(&rawResponse); err != nil {
		http.Error(w, "JSON Decode Failed", 500)
		return
	}

	// --- 1. Sanitization Phase ---
	// Immediately remove any results that don't have real airline data
	var cleanFlights []IxigoResult
	for _, f := range rawResponse.Data.Going.Results {
		if f.Airline != "" && f.FlightNumber != "" && f.AirlineCode != "" {
			cleanFlights = append(cleanFlights, f)
		}
	}

	// --- 2. Filter for Target Date ---
	var targetFlights []IxigoResult
	for _, f := range cleanFlights {
		if f.Date == targetDate {
			targetFlights = append(targetFlights, f)
		}
	}

	// --- 3. Sorting & Limit ---
	sort.Slice(targetFlights, func(i, j int) bool {
		return targetFlights[i].Fare < targetFlights[j].Fare
	})

	limit := 5
	if len(targetFlights) < 5 {
		limit = len(targetFlights)
	}

	// Only send to Discord if a verified flight for the 10th exists
	if len(targetFlights) > 0 {
		sendToDiscord(targetDate, targetFlights[:limit])
	}

	// Output summary
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"success","date":"%s","total_verified_in_response":%d,"target_date_verified_count":%d}`, 
		targetDate, len(cleanFlights), len(targetFlights))
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
				"title":       fmt.Sprintf("✈️ Verified Flight Options for %s", date),
				"description": "SXR ➔ BLR (Verified Carriers Only)",
				"color":       3066993,
				"fields":      fields,
				"footer":      map[string]interface{}{"text": "Vercel Monitor • " + time.Now().Format("15:04")},
			},
		},
	}

	body, _ := json.Marshal(payload)
	http.Post(webhookURL, "application/json", bytes.NewBuffer(body))
}
