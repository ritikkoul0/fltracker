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
	Found        int64   `json:"found"`
}

type IxigoResponse struct {
	Data struct {
		Going struct {
			Results []IxigoResult `json:"results"`
		} `json:"going"`
	} `json:"data"`
}

func Handler(w http.ResponseWriter, r *http.Request) {
	// 1. Define Target and Window
	const layout = "02-01-2006"
	targetStr := "10-05-2026"
	centerDate, _ := time.Parse(layout, targetStr)

	// Create a map of allowed dates (3 days before to 3 days after)
	allowedDates := make(map[string]bool)
	for i := -3; i <= 3; i++ {
		d := centerDate.AddDate(0, 0, i)
		allowedDates[d.Format(layout)] = true
	}

	url := "https://www.ixigo.com/outlook/v1/onward/ranged?departureDate=10052026&destination=BLR&fareClass=e&origin=SXR&paxCombinationType=100&refundTypes=REFUNDABLE%2CNON_REFUNDABLE%2CPARTIALLY_REFUNDABLE"

	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)

	req.Header.Set("accept", "*/*")
	req.Header.Set("apikey", "ixiweb!2$")
	req.Header.Set("clientid", "ixiweb")
	req.Header.Set("ixisrc", "ixiweb")
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

	// 2. Filter: Valid identifiers AND within the 7-day window
	var windowFlights []IxigoResult
	for _, f := range rawResponse.Data.Going.Results {
		if allowedDates[f.Date] && f.Airline != "" && f.AirlineCode != "" && f.FlightNumber != "" {
			windowFlights = append(windowFlights, f)
		}
	}

	// 3. Sort: Cheapest overall within this window
	sort.Slice(windowFlights, func(i, j int) bool {
		return windowFlights[i].Fare < windowFlights[j].Fare
	})

	// 4. Limit to Top 5
	limit := 5
	if len(windowFlights) < 5 {
		limit = len(windowFlights)
	}
	finalSelection := windowFlights[:limit]

	if len(finalSelection) > 0 {
		sendToDiscord(targetStr, finalSelection)
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"success","target":"%s","window_size":7,"found_count":%d}`, targetStr, len(finalSelection))
}

func sendToDiscord(centerDate string, flights []IxigoResult) {
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" {
		return
	}

	var fields []map[string]interface{}
	for i, f := range flights {
		fields = append(fields, map[string]interface{}{
			"name":   fmt.Sprintf("%d. %s - %s", i+1, f.Date, f.Airline),
			"value":  fmt.Sprintf("💰 Fare: **₹%.0f**\n🔢 Flight: `%s` (%s)", f.Fare, f.FlightNumber, f.AirlineCode),
			"inline": false,
		})
	}

	payload := map[string]interface{}{
		"embeds": []interface{}{
			map[string]interface{}{
				"title":       fmt.Sprintf("✈️ Cheapest Flights (±3 Days of %s)", centerDate),
				"description": "SXR ➔ BLR | Only verified carriers included",
				"color":       3066993,
				"fields":      fields,
				"footer":      map[string]interface{}{"text": "Flight Monitor • " + time.Now().Format("15:04")},
			},
		},
	}

	body, _ := json.Marshal(payload)
	http.Post(webhookURL, "application/json", bytes.NewBuffer(body))
}
