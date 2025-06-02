package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Constants for messages and status
const (
	tokenURL           = "https://management.azure.com/"
	invalidNotifType   = "❌ Ongeldig notificatietype, kies 'email' of 'sms'"
	successActionGroup = "✅ Action group succesvol aangemaakt."
	successAlertRule   = "✅ Alert rule succesvol aangemaakt."
)

// Config holds configuration values from the JSON file
type Config struct {
	SubscriptionID      string `json:"subscriptionID"`
	ResourceGroup       string `json:"resourceGroup"`
	ActionGroupLocation string `json:"actionGroupLocation"`
	AlertRuleLocation   string `json:"alertRuleLocation"`
	WorkspaceName       string `json:"workspaceName"`
	ActionGroupName     string `json:"actionGroupName"`
	AlertRuleName       string `json:"alertRuleName"`
}

func loadConfig(filename string) Config {
	file, err := os.ReadFile(filename)
	if err != nil {
		log.Fatalf("Fout bij laden config.json: %v", err)
	}
	var config Config
	if err := json.Unmarshal(file, &config); err != nil {
		log.Fatalf("Fout bij parsen config.json: %v", err)
	}
	return config
}

func getToken() string {
	cmd := exec.Command("az", "account", "get-access-token", "--resource", tokenURL)
	out, err := cmd.Output()
	if err != nil {
		log.Fatal("Fout bij token ophalen:", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		log.Fatal("Fout bij json unmarshal:", err)
	}
	token, ok := result["accessToken"].(string)
	if !ok {
		log.Fatal("Token niet gevonden in response")
	}
	return token
}

func createActionGroup(cfg Config, token, notifType, target string) string {
	url := fmt.Sprintf("https://management.azure.com/subscriptions/%s/resourceGroups/%s/providers/microsoft.insights/actionGroups/%s?api-version=2022-06-01",
		cfg.SubscriptionID, cfg.ResourceGroup, cfg.ActionGroupName)

	properties := map[string]interface{}{
		"groupShortName": cfg.ActionGroupName,
		"enabled":        true,
	}

	if notifType == "email" {
		properties["emailReceivers"] = []map[string]interface{}{
			{
				"name":         "emailReceiver",
				"emailAddress": target,
				"status":       "Enabled",
			},
		}
	} else if notifType == "sms" {
		properties["smsReceivers"] = []map[string]interface{}{
			{
				"name":        "smsReceiver",
				"countryCode": "31",
				"phoneNumber": target,
				"status":      "Enabled",
			},
		}
	} else {
		log.Fatal(invalidNotifType)
	}

	body := map[string]interface{}{
		"location":   cfg.ActionGroupLocation,
		"properties": properties,
	}

	b, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		log.Fatal("Fout bij JSON-marshallen:", err)
	}

	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(b))
	if err != nil {
		log.Fatal("Fout bij aanmaken request:", err) // maak request aan
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second} //  stuurt de request met 15 seconden time out
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("Fout bij uitvoeren request:", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Fatalf("Fout bij aanmaken action group: %s\nResponse body: %s", resp.Status, string(bodyBytes)) // check of request naar api succesvol is , als die niet is error en als die juist is geeft die sycces action group.
	}

	fmt.Println(successActionGroup)
	return fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/microsoft.insights/actionGroups/%s", cfg.SubscriptionID, cfg.ResourceGroup, cfg.ActionGroupName) // retourneent de resource die zojuist is gemaakt
}

func createAlertRule(cfg Config, token, actionGroupID string) {
	url := fmt.Sprintf("https://management.azure.com/subscriptions/%s/resourceGroups/%s/providers/microsoft.insights/scheduledQueryRules/%s?api-version=2023-12-01",
		cfg.SubscriptionID, cfg.ResourceGroup, cfg.AlertRuleName)

	query := "Event | where EventID == 4625 | summarize count() by bin(TimeGenerated, 5m)"

	body := map[string]interface{}{
		"location": cfg.AlertRuleLocation,
		"properties": map[string]interface{}{
			"enabled":             true,
			"description":         "Alert bij mislukte loginpogingen",
			"severity":            3,
			"evaluationFrequency": "PT5M",
			"windowSize":          "PT5M",
			"scopes": []string{
				fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.OperationalInsights/workspaces/%s",
					cfg.SubscriptionID, cfg.ResourceGroup, cfg.WorkspaceName),
			},
			"criteria": map[string]interface{}{
				"allOf": []map[string]interface{}{
					{
						"query":           query,
						"timeAggregation": "Count",
						"operator":        "GreaterThan",
						"threshold":       0,
						"failingPeriods": map[string]interface{}{
							"numberOfEvaluationPeriods": 1,
							"minFailingPeriodsToAlert":  1,
						},
					},
				},
			},
			"actions": map[string]interface{}{
				"actionGroups": []string{actionGroupID},
			},
		},
	}

	b, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		log.Fatal("Fout bij marshallen alert rule:", err)
	}

	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(b))
	if err != nil {
		log.Fatal("Fout bij maken alert rule request:", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("Fout bij uitvoeren alert rule request:", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Fatalf("Fout bij alert rule aanmaken: %s\nResponse body: %s", resp.Status, string(bodyBytes))
	}

	fmt.Println(successAlertRule)
}

func main() {
	config := loadConfig("config.json")
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Wil je notificatie via email of sms? ")
	notifType, _ := reader.ReadString('\n')
	notifType = strings.TrimSpace(strings.ToLower(notifType))

	fmt.Print("Vul het emailadres of telefoonnummer in: ")
	target, _ := reader.ReadString('\n')
	target = strings.TrimSpace(target)

	fmt.Println("🔐 Token ophalen...")
	token := getToken()

	fmt.Println("🔔 Action group aanmaken...")
	actionGroupID := createActionGroup(config, token, notifType, target)

	fmt.Println("📊 Alert rule aanmaken...")
	createAlertRule(config, token, actionGroupID)

	fmt.Println("🎉 Klaar! Je alert en notificatie zijn ingesteld.")
}
