package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Tolfx/UDLDeployer/internal/db"
	"github.com/Tolfx/UDLDeployer/internal/steam"
	"github.com/Tolfx/UDLDeployer/internal/templates"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

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

	// Fetch league matches
	matches, err := db.FetchLeagueMatches(dbConn, []int{0})
	if err != nil {
		fmt.Println("Failed to get league matches")
		panic(err)
	}

	steamService := steam.NewSteamClient(os.Getenv("STEAM_WEB_API_KEY"))

	for _, match := range matches {
		matchRounds, err := db.FetchMatchRounds(dbConn, match.ID)
		if err != nil {
			fmt.Println("Failed to get match rounds")
			panic(err)
		}

		// Fetch division for home team
		division, err := db.FetchDivision(dbConn, match.RosterHomeID)
		if err != nil {
			fmt.Println("Failed to get division")
			panic(err)
		}

		league, err := db.FetchLeague(dbConn, division)
		if err != nil {
			fmt.Println("Failed to get league")
			panic(err)
		}

		// Fetch home team steam IDs
		homeTeamSteamIDs, err := db.FetchTeamSteamIDs(dbConn, match.RosterHomeID)
		if err != nil {
			fmt.Println("Failed to get steam ids from home team")
			panic(err)
		}

		// Fetch away team steam IDs
		awayTeamSteamIDs, err := db.FetchTeamSteamIDs(dbConn, match.RosterAwayID)
		if err != nil {
			fmt.Println("Failed to get steam ids from away team")
			panic(err)
		}

		for _, round := range matchRounds {

			if !round.HomeReady || !round.AwayReady {
				fmt.Printf("Round %d is not ready by both teams, skipping server creation.\n", round.ID)
				continue
			}

			if round.HasOutcome {
				fmt.Printf("Round %d already has an outcome, skipping server creation.\n", round.ID)
				continue
			}

			udlServer, err := templates.NewUdlServer(
				fmt.Sprintf("%d", match.ID),
				division,
				fmt.Sprintf("%d", match.RosterAwayID),
				fmt.Sprintf("%d", match.RosterHomeID),
				strings.Join(strings.Split(awayTeamSteamIDs, ","), ","),
				strings.Join(strings.Split(homeTeamSteamIDs, ","), ","),
				round.ID,
				league.MinPlayers,
				league.MaxPlayers,
			)

			udlServer.SetWinLimit(match.WinLimit)

			if err != nil {
				fmt.Println("Failed to create udl server template")
				panic(err)
			}

			mapName, err := db.FetchMapName(dbConn, round.MapID)
			if err != nil {
				fmt.Println("Failed to get map name")
				panic(err)
			}

			udlServer.SetMap(*mapName)

			// Check if the deployment already exists
			deploymentName := udlServer.GetName()
			cmd := exec.Command("kubectl", "get", "deployment", deploymentName, "-n", "udl", "-o", "jsonpath={.metadata.name}")
			output, err := cmd.Output()
			if err != nil {
				if _, ok := err.(*exec.ExitError); ok {
					fmt.Printf("Deployment %s does not exist, proceeding with creation.\n", deploymentName)
				} else {
					fmt.Println("Error", err)
					panic(err)
				}
			}

			if string(output) == deploymentName {
				fmt.Printf("Deployment %s already exists, skipping creation.\n", deploymentName)
				continue
			}

			// No server, we now need to create custom steam token
			steamToken, err := steamService.CreateAccount(440, deploymentName)
			if err != nil {
				fmt.Println("Failed to create account for server")
				panic(err)
			}

			udlServer.SetSRCDSToken(steamToken.LoginToken)

			renderedTemplate, err := udlServer.RenderTemplate()
			if err != nil {
				panic(err)
			}

			// Create the deployment
			createCmd := exec.Command("kubectl", "apply", "-f", "-")
			createCmd.Stdin = strings.NewReader(renderedTemplate)
			createOutput, err := createCmd.CombinedOutput()
			if err != nil {
				panic(fmt.Sprintf("Failed to create deployment: %s", string(createOutput)))
			}

			fmt.Printf("Deployment %s created successfully.\n", deploymentName)
		}
	}
}
