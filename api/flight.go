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
	Found        int64   `json:"found"` // Timestamp of when the flight was added/found
}

type IxigoResponse struct {
	Data struct {
		Going struct {
			Results []IxigoResult `json:"results"`
		} `json:"going"`
	} `json:"data"`
}

func Handler(w http.ResponseWriter, r *http.Request) {
	url := "https://www.ixigo.com/outlook/v1/onward/ranged?departureDate=10052026&destination=BLR&fareClass=e&origin=SXR&paxCombinationType=100&refundTypes=REFUNDABLE%2CNON_REFUNDABLE%2CPARTIALLY_REFUNDABLE"

	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)

	// Headers matching your successful curl
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

	// 1. Step: Filter out empty placeholders
	var verifiedFlights []IxigoResult
	for _, f := range rawResponse.Data.Going.Results {
		// Only consider if Airline and Flight Number are present
		if f.Airline != "" && f.AirlineCode != "" && f.FlightNumber != "" && f.Fare > 0 {
			verifiedFlights = append(verifiedFlights, f)
		}
	}

	// 2. Step: Sort the entire verified list by Fare (Cheapest first)
	sort.Slice(verifiedFlights, func(i, j int) bool {
		return verifiedFlights[i].Fare < verifiedFlights[j].Fare
	})

	// 3. Step: Take the Top 5 overall cheapest
	limit := 5
	if len(verifiedFlights) < 5 {
		limit = len(verifiedFlights)
	}
	finalSelection := verifiedFlights[:limit]

	// 4. Step: Send to Discord if data exists
	if len(finalSelection) > 0 {
		sendToDiscord(finalSelection)
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"success","total_verified":%d,"top_cheapest_sent":%d}`, len(verifiedFlights), len(finalSelection))
}

func sendToDiscord(flights []IxigoResult) {
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" {
		return
	}

	var fields []map[string]interface{}
	for i, f := range flights {
		// Convert the 'found' timestamp to a readable format if you want to see when it was added
		foundTime := time.Unix(f.Found/1000, 0).Format("02 Jan 15:04")

		fields = append(fields, map[string]interface{}{
			"name":   fmt.Sprintf("%d. %s (%s) - Travel Date: %s", i+1, f.Airline, f.AirlineCode, f.Date),
			"value":  fmt.Sprintf("💰 Fare: **₹%.0f**\n🔢 Flight: `%s`\n🕒 Added: `%s`", f.Fare, f.FlightNumber, foundTime),
			"inline": false,
		})
	}

	payload := map[string]interface{}{
		"embeds": []interface{}{
			map[string]interface{}{
				"title":       "✈️ 5 Cheapest Verified Flights (Any Date)",
				"description": "SXR ➔ BLR ranked by price",
				"color":       3066993,
				"fields":      fields,
				"footer":      map[string]interface{}{"text": "Flight Monitor • " + time.Now().Format("15:04")},
			},
		},
	}

	body, _ := json.Marshal(payload)
	http.Post(webhookURL, "application/json", bytes.NewBuffer(body))
}
