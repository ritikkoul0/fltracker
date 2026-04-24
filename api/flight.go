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
	url := "https://www.ixigo.com/outlook/v1/onward/ranged?departureDate=10052026&destination=BLR&fareClass=e&origin=SXR&paxCombinationType=100&refundTypes=REFUNDABLE%2CNON_REFUNDABLE%2CPARTIALLY_REFUNDABLE"

	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)

	req.Header.Set("apikey", "ixiweb!2$")
	req.Header.Set("clientid", "ixiweb")
	req.Header.Set("ixisrc", "ixiweb")
	req.Header.Set("uuid", "d07889cb18b346a0ac58")
	req.Header.Set("deviceid", "d07889cb18b346a0ac58")
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Connection Error", 500)
		return
	}
	defer resp.Body.Close()

	var result IxigoResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		http.Error(w, "JSON Parse Error", 500)
		return
	}

	// 1. Filter out results with 0 fare (invalid data)
	validFlights := []IxigoResult{}
	for _, f := range result.Data.Going.Results {
		if f.Fare > 0 {
			validFlights = append(validFlights, f)
		}
	}

	// 2. Sort by Fare (Ascending)
	sort.Slice(validFlights, func(i, j int) bool {
		return validFlights[i].Fare < validFlights[j].Fare
	})

	// 3. Get Top 5
	limit := 5
	if len(validFlights) < 5 {
		limit = len(validFlights)
	}
	cheapestFlights := validFlights[:limit]

	if len(cheapestFlights) > 0 {
		sendToDiscord(cheapestFlights)
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"success","sent_to_discord":%d}`, len(cheapestFlights))
}

func sendToDiscord(flights []IxigoResult) {
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" {
		return
	}

	var fields []map[string]interface{}
	for _, f := range flights {
		// Handle empty airline names in ranged view
		airlineName := f.Airline
		if airlineName == "" {
			airlineName = "Multiple/Check Site"
		}

		fields = append(fields, map[string]interface{}{
			"name":  fmt.Sprintf("📅 %s", f.Date),
			"value": fmt.Sprintf("💰 **₹%.0f**\n✈️ %s (%s)", f.Fare, airlineName, f.AirlineCode),
			"inline": true,
		})
	}

	payload := map[string]interface{}{
		"embeds": []interface{}{
			map[string]interface{}{
				"title":       "📉 Top 5 Cheapest Flights: SXR ➔ BLR",
				"description": "Found the lowest fares across your selected date range.",
				"color":       3066993,
				"fields":      fields,
				"footer":      map[string]interface{}{"text": "Vercel Price Tracker • " + time.Now().Format("15:04")},
			},
		},
	}

	body, _ := json.Marshal(payload)
	http.Post(webhookURL, "application/json", bytes.NewBuffer(body))
}
