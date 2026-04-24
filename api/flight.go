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

type IxigoFlight struct {
	Price       float64 `json:"f"`  // Fare
	AirlineCode string  `json:"ak"` // Airline Key
	DepTime     string  `json:"dt"` // Departure Time
	ArrTime     string  `json:"at"` // Arrival Time
}

type IxigoResponse struct {
	Flights []IxigoFlight `json:"flights"`
}

// Handler targets SXR -> BLR for May 10, 2026
func Handler(w http.ResponseWriter, r *http.Request) {
	apiKey := "ixiweb!2$"
	url := "https://www.ixigo.com/outlook/v1/onward/ranged?departureDate=10052026&destination=BLR&fareClass=e&origin=SXR&paxCombinationType=100"

	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	
	// Headers from your successful curl
	req.Header.Set("apikey", apiKey)
	req.Header.Set("clientid", "ixiweb")
	req.Header.Set("ixisrc", "ixiweb")
	req.Header.Set("uuid", "d07889cb18b346a0ac58")
	req.Header.Set("deviceid", "d07889cb18b346a0ac58")

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Failed to reach ixigo", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var result IxigoResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		http.Error(w, "Failed to decode response", http.StatusInternalServerError)
		return
	}

	// Sort by price ascending
	sort.Slice(result.Flights, func(i, j int) bool {
		return result.Flights[i].Price < result.Flights[j].Price
	})

	// Pick top 5
	limit := 5
	if len(result.Flights) < 5 {
		limit = len(result.Flights)
	}
	topFlights := result.Flights[:limit]

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
			"name":  fmt.Sprintf("✈️ %s", f.AirlineCode),
			"value": fmt.Sprintf("Price: `₹%.0f` | Dep: %s", f.Price, f.DepTime),
			"inline": false,
		})
	}

	payload := map[string]interface{}{
		"embeds": []interface{}{
			map[string]interface{}{
				"title":       "✈️ Cheapest Flights: Srinagar (SXR) ➔ Bengaluru (BLR)",
				"description": "**Date:** 10th May 2026",
				"url":         "https://www.ixigo.com/search/result/flight?from=SXR&to=BLR&date=10052026",
				"color":       3447003, // Nice Blue
				"fields":      fields,
				"footer":      map[string]interface{}{"text": "Ixigo Monitor • " + time.Now().Format("15:04")},
			},
		},
	}

	body, _ := json.Marshal(payload)
	http.Post(webhookURL, "application/json", bytes.NewBuffer(body))
}
