package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Tolfx/UDLDeployer/internal/db"
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
		panic(err)
	}

	for _, match := range matches {
		matchRounds, err := db.FetchMatchRounds(dbConn, match.ID)
		if err != nil {
			panic(err)
		}

		// Fetch division for home team
		division, err := db.FetchDivision(dbConn, match.RosterHomeID)
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
			// Check if match details already exist
			existingDetails, err := db.FetchMatchDetails(dbConn, match.ID, round.ID)
			if err != nil {
				panic(err)
			}
			if existingDetails != nil {
				fmt.Printf("Match %d Round %d details already exist, skipping...\n", match.ID, round.ID)
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
			)

			if err != nil {
				panic(err)
			}

			mapName, err := db.FetchMapName(dbConn, round.MapID)
			if err != nil {
				panic(err)
			}

			udlServer.SetMap(*mapName)

			// Check if the deployment already exists
			deploymentName := udlServer.GetName()
			cmd := exec.Command("kubectl", "get", "deployment", deploymentName, "-n", "udl", "-o", "jsonpath={.metadata.name}")
			_, err = cmd.Output()
			if err != nil {
				if _, ok := err.(*exec.ExitError); ok {
					fmt.Printf("Deployment %s does not exist\n", deploymentName)
					return
				} else {
					fmt.Println("Error", err)
					panic(err)
				}
			}

			// Get node external IP and nodePort
			serviceName := deploymentName
			cmd = exec.Command("kubectl", "get", "service", serviceName, "-n", "udl", "-o", "jsonpath={.spec.ports[0].nodePort}")
			nodePortOutput, err := cmd.Output()
			if err != nil {
				fmt.Println("Error getting nodePort:", err)
				panic(err)
			}
			nodePort := strings.TrimSpace(string(nodePortOutput))

			cmd = exec.Command("kubectl", "get", "nodes", "-o", "jsonpath={.items[0].status.addresses[?(@.type=='ExternalIP')].address}")
			nodeIPOutput, err := cmd.Output()
			if err != nil || strings.TrimSpace(string(nodeIPOutput)) == "" {
				// Fallback to internal IP if external IP is not available
				cmd = exec.Command("kubectl", "get", "nodes", "-o", "jsonpath={.items[0].status.addresses[?(@.type=='InternalIP')].address}")
				nodeIPOutput, err = cmd.Output()
				if err != nil {
					fmt.Println("Error getting node internal IP:", err)
					panic(err)
				}
				// Filter out IPv6 addresses, keep only IPv4
				nodeIPs := strings.Split(strings.TrimSpace(string(nodeIPOutput)), " ")
				for _, ip := range nodeIPs {
					if strings.Count(ip, ":") < 2 { // Simple check for IPv4
						nodeIPOutput = []byte(ip)
						break
					}
				}
			}
			nodeIP := strings.TrimSpace(string(nodeIPOutput))

			// Get password from deployment container env
			cmd = exec.Command("kubectl", "get", "deployment", deploymentName, "-n", "udl", "-o", "jsonpath={.spec.template.spec.containers[0].env[?(@.name=='SRCDS_PW')].value}")
			passwordOutput, err := cmd.Output()
			if err != nil {
				fmt.Println("Error getting password:", err)
				panic(err)
			}
			password := strings.TrimSpace(string(passwordOutput))

			fmt.Printf("Match %d Round %d is running on %s:%s with password %s\n", match.ID, round.ID, nodeIP, nodePort, password)

			err = db.CreateMatchDetails(dbConn, match.ID, round.ID, nodeIP, nodePort, password, *mapName)
			if err != nil {
				fmt.Println("Error setting match details:", err)
				panic(err)
			}
		}
	}
}
