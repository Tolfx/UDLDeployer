package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"

	"github.com/Tolfx/UDLDeployer/internal/db"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type ScoreData struct {
	MatchID      int `json:"match_id"`
	RoundID      int `json:"round_id"`
	WinnerTeamID int `json:"winner_team_id"`
	LoserTeamID  int `json:"loser_team_id"`
	AwayPoints   int `json:"away_points"`
	HomePoints   int `json:"home_points"`
}

func updateScores(matchID int) error {
	updateScoresURL := fmt.Sprintf("https://udl.tf/leagues/matches/%d/update_scores", matchID)
	req, err := http.NewRequest(http.MethodPost, updateScoresURL, nil)
	if err != nil {
		return fmt.Errorf("error creating request to update scores: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return fmt.Errorf("error updating scores: %w", err)
	}
	return nil
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
		err = db.UpdateMatchRound(dbConn, scoreData.RoundID, scoreData.WinnerTeamID, scoreData.LoserTeamID, scoreData.HomePoints, scoreData.AwayPoints)

		if err != nil {
			http.Error(w, "Error updating data", http.StatusBadRequest)
			fmt.Println("Error", err)
			return
		}

		if err := updateScores(scoreData.MatchID); err != nil {
			http.Error(w, "Error updating scores", http.StatusInternalServerError)
			fmt.Println("Error", err)
			return
		}

		allDone, _ := db.AreAllRoundsDone(dbConn, scoreData.MatchID)

		if allDone {
			err = db.UpdateMatchStatus(dbConn, scoreData.MatchID, 3)
			if err != nil {
				http.Error(w, "Error updating match status", http.StatusBadRequest)
				fmt.Println("Error", err)
				return
			}
			if err := updateScores(scoreData.MatchID); err != nil {
				http.Error(w, "Error updating scores", http.StatusInternalServerError)
				fmt.Println("Error", err)
				return
			}
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Score data received"))
	})

	http.HandleFunc("/upload-demo", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
			return
		}

		querySecretPassword := r.URL.Query().Get("secret_password")
		if querySecretPassword != secretPassword {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "Error retrieving the file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		// Create a folder to save the uploaded files if it doesn't exist
		uploadPath := os.Getenv("UPLOAD_PATH")
		if uploadPath == "" {
			uploadPath = "./upload"
		}
		if _, err := os.Stat(uploadPath); os.IsNotExist(err) {
			err = os.Mkdir(uploadPath, os.ModePerm)
			if err != nil {
				http.Error(w, "Error creating upload directory", http.StatusInternalServerError)
				return
			}
		}

		// Create a file in the upload directory with the same name as the uploaded file
		dst, err := os.Create(fmt.Sprintf("%s/%s", uploadPath, header.Filename))
		if err != nil {
			http.Error(w, "Error creating the file", http.StatusInternalServerError)
			return
		}
		defer dst.Close()

		// Copy the uploaded file to the destination file
		_, err = io.Copy(dst, file)
		if err != nil {
			http.Error(w, "Error saving the file", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Demo file uploaded successfully"))
	})

	http.HandleFunc("/get-demo", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
			return
		}

		matchID := r.URL.Query().Get("match_id")
		roundID := r.URL.Query().Get("round_id")
		if matchID == "" || roundID == "" {
			http.Error(w, "Missing match_id or round_id", http.StatusBadRequest)
			return
		}

		uploadPath := os.Getenv("DEMO_PATH")
		if uploadPath == "" {
			uploadPath = "./demo"
		}

		// Find the file with the given match ID and round ID using regex
		filePattern := fmt.Sprintf(`match-%s-round-%s`, matchID, roundID)
		files, err := os.ReadDir(uploadPath)
		if err != nil {
			http.Error(w, "Error reading demo directory", http.StatusInternalServerError)
			return
		}

		var demoFile string
		var maxSize int64
		for _, file := range files {
			if !file.IsDir() && matchRegex(file.Name(), filePattern) {
				fileInfo, err := file.Info()
				if err != nil {
					continue
				}
				if fileInfo.Size() > maxSize {
					maxSize = fileInfo.Size()
					demoFile = file.Name()
				}
			}
		}

		if demoFile == "" {
			http.Error(w, "Demo file not found", http.StatusNotFound)
			return
		}

		fmt.Printf("Downloading demo file: %s\n", demoFile)

		// Set the original file name in the response header
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", demoFile))
		http.ServeFile(w, r, fmt.Sprintf("%s/%s", uploadPath, demoFile))
	})

	// POST /player-match-statistics: Upsert player match statistics
	http.HandleFunc("/player-match-statistics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
			return
		}
		querySecretPassword := r.URL.Query().Get("secret_password")
		if querySecretPassword != secretPassword {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		type reqBody struct {
			SteamID          int64 `json:"steam_id"`
			LeagueMatchID    int64 `json:"league_match_id"`
			Kills            int   `json:"kills"`
			Deaths           int   `json:"deaths"`
			Deflects         int   `json:"deflects"`
			TimeAliveSeconds int   `json:"time_alive_seconds"`
		}
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Error parsing JSON data", http.StatusBadRequest)
			return
		}
		stat := db.PlayerMatchStatistic{
			SteamID:          body.SteamID,
			LeagueMatchID:    body.LeagueMatchID,
			Kills:            body.Kills,
			Deaths:           body.Deaths,
			Deflects:         body.Deflects,
			TimeAliveSeconds: body.TimeAliveSeconds,
		}
		if err := db.UpsertPlayerMatchStatistic(dbConn, stat); err != nil {
			http.Error(w, "Error upserting player match statistic", http.StatusInternalServerError)
			fmt.Println("Error:", err)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Player match statistic upserted"))
	})

	// POST /player-chat-logs: Batch insert player chat logs
	http.HandleFunc("/player-chat-logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
			return
		}
		querySecretPassword := r.URL.Query().Get("secret_password")
		if querySecretPassword != secretPassword {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		type chatLog struct {
			SteamID       int64  `json:"steam_id"`
			LeagueMatchID int64  `json:"league_match_id"`
			Message       string `json:"message"`
			SentAt        string `json:"sent_at"` // ISO8601 string
		}
		var logs []chatLog
		if err := json.NewDecoder(r.Body).Decode(&logs); err != nil {
			http.Error(w, "Error parsing JSON data", http.StatusBadRequest)
			return
		}
		dbLogs := make([]db.PlayerChatLog, 0, len(logs))
		for _, l := range logs {
			dbLogs = append(dbLogs, db.PlayerChatLog{
				SteamID:       l.SteamID,
				LeagueMatchID: l.LeagueMatchID,
				Message:       l.Message,
				SentAt:        l.SentAt,
			})
		}
		if err := db.InsertPlayerChatLogs(dbConn, dbLogs); err != nil {
			http.Error(w, "Error inserting player chat logs", http.StatusInternalServerError)
			fmt.Println("Error:", err)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Player chat logs inserted"))
	})

	serverPort := "5823"

	fmt.Printf("Starting server on port %s...\n", serverPort)
	if err := http.ListenAndServe(":"+serverPort, nil); err != nil {
		panic(err)
	}
}

// Helper function to check if a string matches a regex pattern
func matchRegex(str, pattern string) bool {
	matched, _ := regexp.MatchString(pattern, str)
	return matched
}
