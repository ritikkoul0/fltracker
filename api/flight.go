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

// --- Updated Models to match Ixigo Response ---

type IxigoFlight struct {
	Price       float64 `json:"f"`  // Fare
	AirlineCode string  `json:"ak"` // Airline Key
	DepTime     string  `json:"dt"` // Departure Time
	ArrTime     string  `json:"at"` // Arrival Time
}

type IxigoResponse struct {
	// The /ranged API often wraps results in an 'onwardFlights' array 
	// or a 'flights' map. This structure accounts for the standard flight array.
	Flights []IxigoFlight `json:"flights"`
}

func Handler(w http.ResponseWriter, r *http.Request) {
	// 1. Setup Request
	url := "https://www.ixigo.com/outlook/v1/onward/ranged?departureDate=10052026&destination=BLR&fareClass=e&origin=SXR&paxCombinationType=100&refundTypes=REFUNDABLE%2CNON_REFUNDABLE%2CPARTIALLY_REFUNDABLE"
	
	client := &http.Client{Timeout: 20 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	
	// 2. Mandatory Headers (Mimicking your successful Browser Curl)
	req.Header.Set("accept", "*/*")
	req.Header.Set("apikey", "ixiweb!2$")
	req.Header.Set("clientid", "ixiweb")
	req.Header.Set("ixisrc", "ixiweb")
	req.Header.Set("uuid", "d07889cb18b346a0ac58")
	req.Header.Set("deviceid", "d07889cb18b346a0ac58")
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36")
	req.Header.Set("referer", "https://www.ixigo.com/search/result/flight?from=SXR&to=BLR&date=10052026")

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Request failed", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// 3. Decode Response
	var result IxigoResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		http.Error(w, "JSON Decode failed", http.StatusInternalServerError)
		return
	}

	// 4. Sort and Filter
	sort.Slice(result.Flights, func(i, j int) bool {
		return result.Flights[i].Price < result.Flights[j].Price
	})

	limit := 5
	if len(result.Flights) < 5 {
		limit = len(result.Flights)
	}
	topFlights := result.Flights[:limit]

	// 5. Notify if flights found
	if len(topFlights) > 0 {
		sendToDiscord(topFlights)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"success","count":%d}`, len(topFlights))
}

func sendToDiscord(flights []IxigoFlight) {
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" {
		return
	}

	var fields []map[string]interface{}
	for _, f := range flights {
		fields = append(fields, map[string]interface{}{
			"name":   fmt.Sprintf("✈️ %s", f.AirlineCode),
			"value":  fmt.Sprintf("Price: **₹%.0f**\nDep: `%s` | Arr: `%s`", f.Price, f.DepTime, f.ArrTime),
			"inline": true,
		})
	}

	payload := map[string]interface{}{
		"embeds": []interface{}{
			map[string]interface{}{
				"title":       "⚡ SXR ➔ BLR Price Alert",
				"description": "Travel Date: **10 May 2026**",
				"url":         "https://www.ixigo.com/search/result/flight?from=SXR&to=BLR&date=10052026",
				"color":       3066993,
				"fields":      fields,
				"footer":      map[string]interface{}{"text": "Vercel Ixigo Bot • " + time.Now().Format("15:04")},
			},
		},
	}

	body, _ := json.Marshal(payload)
	http.Post(webhookURL, "application/json", bytes.NewBuffer(body))
}
