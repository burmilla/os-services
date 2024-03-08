package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

var (
	tenantId     string
	clientId     string
	clientSecret string
)

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int    `json:"expires_in"`
	ExtExpiresIn     int    `json:"ext_expires_in"`
	TokenType        string `json:"token_type"`
	ErrorDescription string `json:"error_description"`
}

type authResponse struct {
	AccessToken  string `json:"access_token"`
	ClientID     string `json:"client_id"`
	ExpiresIn    string `json:"expires_in"`
	ExpiresOn    string `json:"expires_on"`
	ExtExpiresIn string `json:"ext_expires_in"`
	NotBefore    string `json:"not_before"`
	Resource     string `json:"resource"`
	TokenType    string `json:"token_type"`
}

func authHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/metadata/identity/oauth2/token" {
		http.NotFound(w, r)
		return
	}

	queryParams := r.URL.Query()
	resource := queryParams.Get("resource")
	if resource == "" {
		http.Error(w, "Missing resource parameter", http.StatusBadRequest)
		return
	}
	clientRequestID := r.Header.Get("x-ms-client-request-id")
	log.Printf("Client: %s, Client Request ID: %s, Requested Resource: %s\n", r.RemoteAddr, clientRequestID, resource)
	scope := resource + "/.default"

	currentTime := time.Now()
	tokenResponseJson, err := fetchAzureToken(tenantId, clientId, clientSecret, scope)
	if err != nil {
		log.Printf("Error fetching Azure token: %v", err)
		http.Error(w, "Failed to fetch Azure token", http.StatusInternalServerError)
		return
	}

	var azureTokenResponse tokenResponse
	if err := json.Unmarshal([]byte(tokenResponseJson), &azureTokenResponse); err != nil {
		log.Printf("Error unmarshaling Azure token response: %v", err)
		http.Error(w, "Error processing token response", http.StatusInternalServerError)
		return
	}

	if azureTokenResponse.ErrorDescription != "" {
		log.Printf("Failed to token from Azure: %v", azureTokenResponse.ErrorDescription)
		http.Error(w, "Failed to token from Azure", http.StatusInternalServerError)
		return
	}

	expiresOn := currentTime.Add(time.Second * time.Duration(azureTokenResponse.ExpiresIn)).Unix()
	notBefore := currentTime.Unix()

	clientResponse := authResponse{
		AccessToken:  azureTokenResponse.AccessToken,
		ExpiresIn:    strconv.Itoa(azureTokenResponse.ExpiresIn),
		ExpiresOn:    strconv.FormatInt(expiresOn, 10),
		ExtExpiresIn: strconv.Itoa(azureTokenResponse.ExtExpiresIn),
		NotBefore:    strconv.FormatInt(notBefore, 10),
		Resource:     resource,
		TokenType:    azureTokenResponse.TokenType,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(clientResponse)
}

func fetchAzureToken(tenantID, clientID, clientSecret, scope string) (string, error) {
	tokenURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", tenantID)

	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("scope", scope)
	data.Set("grant_type", "client_credentials")

	req, err := http.NewRequest("POST", tokenURL, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return "", err
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func main() {
	tenantId = os.Getenv("AZIDENTITY_TENANTID")
	clientId = os.Getenv("AZIDENTITY_CLIENTID")
	clientSecret = os.Getenv("AZIDENTITY_SECRET")

	if tenantId == "" || clientId == "" || clientSecret == "" {
		log.Fatal("Mandatory environment variables not defined")
	}
	http.HandleFunc("/metadata/identity/oauth2/token", authHandler)
	log.Println("Listening requests on http://169.254.169.254")
	if err := http.ListenAndServe("169.254.169.254:80", nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
