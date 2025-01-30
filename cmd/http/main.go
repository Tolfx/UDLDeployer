package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/Tolfx/UDLDeployer/internal/db"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type ScoreData struct {
	MatchID      int `json:"match_id"`
	RoundID      int `json:"round_id"`
	WinnerTeamID int `json:"winner_team_id"`
	LoserTeamID  int `json:"loser_team_id"`
	WinnerPoints int `json:"winner_points"`
	LoserPoints  int `json:"loser_points"`
}

func main() {
	// Load .env file
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Error loading .env file")
	}

	// Get environment variables
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")

	secretPassword := os.Getenv("SECRET_PASSWORD")

	if secretPassword == "" {
		panic("SECRET_PASSWORD environment variable is not set")
	}

	// Create connection string
	psqlInfo := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	// Connect to the database
	dbConn, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		panic(err)
	}
	defer dbConn.Close()

	// Verify connection
	err = dbConn.Ping()
	if err != nil {
		panic(err)
	}

	fmt.Println("Successfully connected to the database!")

	// Set up HTTP server
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("API is up and running"))
	})

	http.HandleFunc("/send-scores", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
			return
		}

		querySecretPassword := r.URL.Query().Get("secret_password")
		if querySecretPassword != secretPassword {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		var scoreData ScoreData
		err := json.NewDecoder(r.Body).Decode(&scoreData)
		if err != nil {
			http.Error(w, "Error parsing JSON data", http.StatusBadRequest)
			return
		}

		fmt.Printf("Received score data: %+v\n", scoreData)

		// Update database
		err = db.UpdateMatchRound(dbConn, scoreData.MatchID, scoreData.WinnerTeamID, scoreData.LoserTeamID, scoreData.WinnerPoints, scoreData.LoserPoints)

		if err != nil {
			http.Error(w, "Error updating data", http.StatusBadRequest)
			fmt.Println("Error", err)
			return
		}

		err = db.UpdateMatchStatus(dbConn, scoreData.MatchID, 3)
		if err != nil {
			http.Error(w, "Error updating match status", http.StatusBadRequest)
			fmt.Println("Error", err)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Score data received"))
	})

	serverPort := "5823"

	fmt.Printf("Starting server on port %s...\n", serverPort)
	if err := http.ListenAndServe(":"+serverPort, nil); err != nil {
		panic(err)
	}
}
