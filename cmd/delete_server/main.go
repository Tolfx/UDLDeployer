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
	matches, err := db.FetchLeagueMatches(dbConn, []int{1, 2, 3})
	if err != nil {
		panic(err)
	}

	steamService := steam.NewSteamClient(os.Getenv("STEAM_WEB_API_KEY"))
	accounts, _ := steamService.GetAccountList()

	for _, match := range matches {
		// Fetch match rounds
		matchRounds, err := db.FetchMatchRounds(dbConn, match.ID)
		if err != nil {
			panic(err)
		}

		// Fetch division for home team
		division, err := db.FetchDivision(dbConn, match.RosterHomeID)
		if err != nil {
			panic(err)
		}

		league, err := db.FetchLeague(dbConn, division)
		if err != nil {
			panic(err)
		}

		// Fetch home team steam IDs
		homeTeamSteamIDs, err := db.FetchTeamSteamIDs(dbConn, match.RosterHomeID)
		if err != nil {
			panic(err)
		}

		// Fetch away team steam IDs
		awayTeamSteamIDs, err := db.FetchTeamSteamIDs(dbConn, match.RosterAwayID)
		if err != nil {
			panic(err)
		}

		for _, round := range matchRounds {
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

			if err != nil {
				panic(err)
			}

			// Check if the deployment exists, if it does delete
			deploymentName := udlServer.GetName()
			cmd := exec.Command("kubectl", "get", "deployment", deploymentName, "-n", "udl", "-o", "jsonpath={.metadata.name}")
			_, err = cmd.Output()
			if err != nil {
				if _, ok := err.(*exec.ExitError); ok {
					fmt.Printf("Deployment %s does not exist.\n", deploymentName)
				} else {
					fmt.Println("Error", err)
					panic(err)
				}
			} else {
				fmt.Printf("Deployment %s exists, fetching SRCDS token.\n", deploymentName)
				tokenCmd := exec.Command("kubectl", "get", "deployment", deploymentName, "-n", "udl", "-o", "jsonpath={.spec.template.spec.containers[0].env[?(@.name=='SRCDS_TOKEN')].value}")
				tokenOutput, tokenErr := tokenCmd.Output()
				if tokenErr != nil {
					fmt.Println("Error fetching SRCDS token:", tokenErr)
					panic(tokenErr)
				}
				srcdsToken := string(tokenOutput)
				fmt.Printf("SRCDS token for deployment %s: %s\n", deploymentName, srcdsToken)

				var steamID string
				for _, account := range accounts {
					if account.LoginToken == srcdsToken {
						steamID = account.SteamID
						break
					}
				}
				if steamID == "" {
					fmt.Printf("No account found with SRCDS token %s.\n", srcdsToken)
				} else {
					fmt.Printf("SteamID for SRCDS token %s: %s\n", srcdsToken, steamID)
				}

				// Delete the SRCDS token from the account
				err = steamService.DeleteAccount(steamID)
				if err != nil {
					fmt.Println("Error deleting SRCDS token:", err)
					panic(err)
				}
				fmt.Printf("SRCDS token %s deleted successfully.\n", srcdsToken)

				fmt.Printf("Deleting deployment %s.\n", deploymentName)
				deleteCmd := exec.Command("kubectl", "delete", "deployment", deploymentName, "-n", "udl")
				deleteOutput, deleteErr := deleteCmd.CombinedOutput()
				if deleteErr != nil {
					fmt.Println("Error deleting deployment:", string(deleteOutput))
					panic(deleteErr)
				}
				fmt.Printf("Deployment %s deleted successfully.\n", deploymentName)

				fmt.Printf("Deleting service %s.\n", deploymentName)
				deleteServiceCmd := exec.Command("kubectl", "delete", "service", deploymentName, "-n", "udl")
				deleteServiceOutput, deleteServiceErr := deleteServiceCmd.CombinedOutput()
				if deleteServiceErr != nil {
					fmt.Println("Error deleting service:", string(deleteServiceOutput))
					panic(deleteServiceErr)
				}
				fmt.Printf("Service %s deleted successfully.\n", deploymentName)

				err = db.DeleteMatchDetails(dbConn, round.MatchID, round.ID)
				if err != nil {
					fmt.Println("Error deleting match details: ", err)
					panic(err)
				}
			}
		}
	}
}
