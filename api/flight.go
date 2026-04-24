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
	// Target date for the flight search
	targetDate := "10-05-2026" 
	url := "https://www.ixigo.com/outlook/v1/onward/ranged?departureDate=10052026&destination=BLR&fareClass=e&origin=SXR&paxCombinationType=100"

	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)

	// Required Headers for Ixigo API
	req.Header.Set("apikey", "ixiweb!2$")
	req.Header.Set("clientid", "ixiweb")
	req.Header.Set("uuid", "d07889cb18b346a0ac58")
	req.Header.Set("deviceid", "d07889cb18b346a0ac58")
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "API Connection Failed", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var result IxigoResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		http.Error(w, "JSON Decode Failed", http.StatusInternalServerError)
		return
	}

	// 1. Filter: Match Date AND Ensure Airline info is NOT empty
	var dayFlights []IxigoResult
	for _, f := range result.Data.Going.Results {
		// Strict check: Date must match and we must have a valid Airline & Flight Number
		if f.Date == targetDate && f.Airline != "" && f.FlightNumber != "" {
			dayFlights = append(dayFlights, f)
		}
	}

	// 2. Sort by Fare (Cheapest first)
	sort.Slice(dayFlights, func(i, j int) bool {
		return dayFlights[i].Fare < dayFlights[j].Fare
	})

	// 3. Take Top 5
	limit := 5
	if len(dayFlights) < 5 {
		limit = len(dayFlights)
	}

	var finalSelection []IxigoResult
	if len(dayFlights) > 0 {
		finalSelection = dayFlights[:limit]
		sendToDiscord(targetDate, finalSelection)
	}

	// Output result summary to the browser/logs
	w.Header().Set("Content-Type", "application/json")
	responseMsg := fmt.Sprintf(`{"status":"success","date":"%s","verified_count":%d}`, targetDate, len(finalSelection))
	w.Write([]byte(responseMsg))
}

func sendToDiscord(date string, flights []IxigoResult) {
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" {
		fmt.Println("Error: DISCORD_WEBHOOK_URL not set")
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
				"color":       3066993, // Greenish-Blue
				"fields":      fields,
				"footer":      map[string]interface{}{"text": "Flight Monitor • " + time.Now().Format("15:04")},
			},
		},
	}

	body, _ := json.Marshal(payload)
	_, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(body))
	if err != nil {
		fmt.Printf("Error sending to Discord: %v\n", err)
	}
}
