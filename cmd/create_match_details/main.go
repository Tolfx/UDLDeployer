package main

import (
	"database/sql"
	"encoding/json"
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
		fmt.Println("Failed to get league matches")
		panic(err)
	}

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
				league.MinPlayers,
				league.MaxPlayers,
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
					continue
				} else {
					fmt.Println("Error", err)
					panic(err)
				}
			}

			// Get node external IP and nodePort
			serviceName := deploymentName

			// Fetch the full service JSON and parse it so we can handle missing nodePort gracefully
			cmd = exec.Command("kubectl", "get", "service", serviceName, "-n", "udl", "-o", "json")
			serviceJSON, err := cmd.Output()
			if err != nil {
				stderr := ""
				if exitErr, ok := err.(*exec.ExitError); ok {
					stderr = string(exitErr.Stderr)
				}
				fmt.Printf("Error getting service %s: %v\nkubectl stderr: %s\n", serviceName, err, stderr)
				panic(err)
			}

			// Minimal struct to parse ports
			var svc struct {
				Spec struct {
					Ports []struct {
						NodePort *int `json:"nodePort"`
						Port     int  `json:"port"`
					} `json:"ports"`
				} `json:"spec"`
			}
			if err := json.Unmarshal(serviceJSON, &svc); err != nil {
				fmt.Println("Error parsing service JSON:", err)
				panic(err)
			}

			nodePort := ""
			if len(svc.Spec.Ports) > 0 {
				// Prefer NodePort if present (service type NodePort); otherwise fall back to port
				if svc.Spec.Ports[0].NodePort != nil && *svc.Spec.Ports[0].NodePort != 0 {
					nodePort = fmt.Sprintf("%d", *svc.Spec.Ports[0].NodePort)
				} else {
					nodePort = fmt.Sprintf("%d", svc.Spec.Ports[0].Port)
				}
			} else {
				fmt.Printf("Service %s has no ports defined\n", serviceName)
				panic(fmt.Errorf("no ports on service %s", serviceName))
			}

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

			// Get sourcetvport
			cmd = exec.Command("kubectl", "get", "deployment", deploymentName, "-n", "udl", "-o", "jsonpath={.spec.template.spec.containers[0].env[?(@.name=='SRCDS_TV_PORT')].value}")
			sourcetvportOutput, err := cmd.Output()
			if err != nil {
				fmt.Println("Error getting password:", err)
				panic(err)
			}
			sourcetvport := strings.TrimSpace(string(sourcetvportOutput))

			fmt.Printf("Match %d Round %d is running on %s:%s with password %s\n", match.ID, round.ID, nodeIP, nodePort, password)

			err = db.CreateMatchDetails(dbConn, match.ID, round.ID, nodeIP, nodePort, sourcetvport, password, *mapName)
			if err != nil {
				fmt.Println("Error setting match details:", err)
				panic(err)
			}

			message := fmt.Sprintf("Match %d Round %d is running on %s:%s with password %s", match.ID, round.ID, nodeIP, nodePort, password)
			link := fmt.Sprintf("/matches/%d", match.ID)
			db.SendNotificationsToTeams(dbConn, match.RosterHomeID, match.RosterAwayID, message, link)
		}
	}
}
