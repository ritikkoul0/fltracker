package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// --- Models matching your JSON output ---

type IxigoResult struct {
	Airline     string  `json:"airline"`
	AirlineCode string  `json:"airlineCode"`
	Date        string  `json:"date"`
	Fare        float64 `json:"fare"`
}

type IxigoResponse struct {
	Data struct {
		Going struct {
			Results []IxigoResult `json:"results"`
		} `json:"going"`
	} `json:"data"`
}

func Handler(w http.ResponseWriter, r *http.Request) {
	targetDate := "10-05-2026"
	url := "https://www.ixigo.com/outlook/v1/onward/ranged?departureDate=10052026&destination=BLR&fareClass=e&origin=SXR&paxCombinationType=100&refundTypes=REFUNDABLE%2CNON_REFUNDABLE%2CPARTIALLY_REFUNDABLE"

	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)

	// Essential Headers
	req.Header.Set("apikey", "ixiweb!2$")
	req.Header.Set("clientid", "ixiweb")
	req.Header.Set("ixisrc", "ixiweb")
	req.Header.Set("uuid", "d07889cb18b346a0ac58")
	req.Header.Set("deviceid", "d07889cb18b346a0ac58")
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "API Fail", 500)
		return
	}
	defer resp.Body.Close()

	var result IxigoResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		http.Error(w, "JSON Fail", 500)
		return
	}

	// Filter for your specific date
	var foundFlight *IxigoResult
	for _, f := range result.Data.Going.Results {
		if f.Date == targetDate {
			foundFlight = &f
			break
		}
	}

	if foundFlight != nil {
		sendToDiscord(*foundFlight)
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"success","date":"%s","found":%v}`, targetDate, foundFlight != nil)
}

func sendToDiscord(f IxigoResult) {
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" {
		return
	}

	// Using the embed style from your Flipkart tracker
	payload := map[string]interface{}{
		"embeds": []interface{}{
			map[string]interface{}{
				"title":       "✈️ Flight Price Alert: SXR ➔ BLR",
				"description": fmt.Sprintf("Price for **%s**", f.Date),
				"color":       3066993,
				"fields": []map[string]interface{}{
					{"name": "💰 Fare", "value": fmt.Sprintf("`₹%.0f`", f.Fare), "inline": true},
					{"name": "🏢 Airline", "value": f.Airline, "inline": true},
				},
				"footer": map[string]interface{}{"text": "Ixigo Monitor • " + time.Now().Format("15:04")},
			},
		},
	}

	body, _ := json.Marshal(payload)
	http.Post(webhookURL, "application/json", bytes.NewBuffer(body))
}
