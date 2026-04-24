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
	// 1. Setup the Date Window (±3 Days of May 10)
	const layout = "02-01-2006"
	targetStr := "10-05-2026"
	centerDate, _ := time.Parse(layout, targetStr)

	// Define range: 07-05-2026 to 13-05-2026
	allowedDates := make(map[string]bool)
	for i := -3; i <= 3; i++ {
		d := centerDate.AddDate(0, 0, i)
		allowedDates[d.Format(layout)] = true
	}

	url := "https://www.ixigo.com/outlook/v1/onward/ranged?departureDate=10052026&destination=BLR&fareClass=e&origin=SXR&paxCombinationType=100&refundTypes=REFUNDABLE%2CNON_REFUNDABLE%2CPARTIALLY_REFUNDABLE"

	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)

	// Headers
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

	// 2. Filter: Grab everything in the 7-day window
	var windowFlights []IxigoResult
	for _, f := range rawResponse.Data.Going.Results {
		if allowedDates[f.Date] {
			windowFlights = append(windowFlights, f)
		}
	}

	// 3. Sort: Chronological Order (By Date Ascending)
	sort.Slice(windowFlights, func(i, j int) bool {
		t1, _ := time.Parse(layout, windowFlights[i].Date)
		t2, _ := time.Parse(layout, windowFlights[j].Date)
		return t1.Before(t2)
	})

	// 4. Send the results to Discord (No limit, or keep it to 7 for the whole week)
	if len(windowFlights) > 0 {
		sendToDiscord(targetStr, windowFlights)
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"success","info":"Sorted by date ascending","count":%d}`, len(windowFlights))
}

func sendToDiscord(centerDate string, flights []IxigoResult) {
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" {
		return
	}

	var fields []map[string]interface{}
	for _, f := range flights {
		airline := f.Airline
		if airline == "" {
			airline = "Details Pending"
		}
		
		flightNum := f.FlightNumber
		if flightNum == "" {
			flightNum = "TBD"
		}

		fields = append(fields, map[string]interface{}{
			"name":   fmt.Sprintf("📅 %s", f.Date),
			"value":  fmt.Sprintf("💰 Fare: **₹%.0f**\n✈️ %s (`%s`)", f.Fare, airline, flightNum),
			"inline": true, // Using inline true to save vertical space for a weekly view
		})
	}

	payload := map[string]interface{}{
		"embeds": []interface{}{
			map[string]interface{}{
				"title":       fmt.Sprintf("✈️ Weekly Fare Outlook: %s", centerDate),
				"description": "SXR ➔ BLR (Window: -3 to +3 days)",
				"color":       3447003, // Nice Blue
				"fields":      fields,
				"footer":      map[string]interface{}{"text": "Vercel Monitor • " + time.Now().Format("15:04")},
			},
		},
	}

	body, _ := json.Marshal(payload)
	http.Post(webhookURL, "application/json", bytes.NewBuffer(body))
}
