package db

import (
	"database/sql"
	"fmt"

	"github.com/lib/pq"
)

type Match struct {
	ID           int
	RosterAwayID int
	RosterHomeID int
}

type MatchRound struct {
	ID              int
	MatchID         int
	MapID           int
	HomeTeamScore   int
	AwayTeamScore   int
	LoserID         *int
	WinnerID        *int
	HasOutcome      bool
	ScoreDifference float32
}

type MatchDetails struct {
	MatchID  int
	ServerIP string
	Port     string
	Password string
	Map      string
}

type UserNotification struct {
	ID        int
	UserID    int
	Read      bool
	Message   string
	Link      string
	CreatedAt string
	UpdatedAt string
}

type League struct {
	MinPlayers int
	MaxPlayers int
}

func FetchLeagueMatches(db *sql.DB, statuses []int) ([]Match, error) {
	query := `
	SELECT id, home_team_id, away_team_id
	FROM league_matches
	WHERE status = ANY($1) AND home_team_id IS NOT NULL AND away_team_id IS NOT NULL
	`
	rows, err := db.Query(query, pq.Array(statuses))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matches []Match
	for rows.Next() {
		var match Match
		err := rows.Scan(&match.ID, &match.RosterHomeID, &match.RosterAwayID)
		if err != nil {
			return nil, err
		}
		matches = append(matches, match)
	}

	err = rows.Err()
	if err != nil {
		return nil, err
	}

	return matches, nil
}

func FetchDivision(db *sql.DB, rosterId int) (string, error) {
	var division string
	err := db.QueryRow(`
	SELECT division_id
	FROM league_rosters
	WHERE id = $1
	`, rosterId).Scan(&division)
	if err != nil {
		return "", err
	}
	return division, nil
}

func FetchLeague(db *sql.DB, divisionId string) (*League, error) {
	var league League
	var leagueID int
	err := db.QueryRow(`
	SELECT league_id
	FROM league_divisions
	WHERE id = $1
	`, divisionId).Scan(&leagueID)
	if err != nil {
		return nil, err
	}

	err = db.QueryRow(`
	SELECT min_players, max_players
	FROM leagues
	WHERE id = $1
	`, leagueID).Scan(&league.MinPlayers, &league.MaxPlayers)
	if err != nil {
		return nil, err
	}
	return &league, nil
}

func FetchTeamSteamIDs(db *sql.DB, rosterId int) (string, error) {
	query := `
	SELECT string_agg(DISTINCT users.steam_id::text, ',')
	FROM league_roster_players lrp
	LEFT JOIN users ON users.id = lrp.user_id
	WHERE lrp.roster_id = $1
	`
	var steamIDs string
	err := db.QueryRow(query, rosterId).Scan(&steamIDs)
	if err != nil {
		return "", err
	}
	return steamIDs, nil
}

func FetchTeamUserIDs(db *sql.DB, rosterId int) ([]int, error) {
	query := `
	SELECT user_id
	FROM league_roster_players
	WHERE roster_id = $1
	`
	rows, err := db.Query(query, rosterId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var userIDs []int
	for rows.Next() {
		var userID int
		err := rows.Scan(&userID)
		if err != nil {
			return nil, err
		}
		userIDs = append(userIDs, userID)
	}

	err = rows.Err()
	if err != nil {
		return nil, err
	}

	return userIDs, nil
}

func FetchMatchRounds(db *sql.DB, matchID int) ([]MatchRound, error) {
	query := `
	SELECT id, match_id, map_id, home_team_score, away_team_score, loser_id, winner_id, has_outcome, score_difference
	FROM league_match_rounds
	WHERE match_id = $1
	`
	rows, err := db.Query(query, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var matchRounds []MatchRound
	for rows.Next() {
		var matchRound MatchRound
		err := rows.Scan(&matchRound.ID, &matchRound.MatchID, &matchRound.MapID, &matchRound.HomeTeamScore, &matchRound.AwayTeamScore, &matchRound.LoserID, &matchRound.WinnerID, &matchRound.HasOutcome, &matchRound.ScoreDifference)
		if err != nil {
			return nil, err
		}
		matchRounds = append(matchRounds, matchRound)
	}

	err = rows.Err()
	if err != nil {
		return nil, err
	}

	return matchRounds, nil
}

func FetchMapName(db *sql.DB, mapId int) (*string, error) {
	var mapName string
	err := db.QueryRow(`
	SELECT name
	FROM maps
	WHERE id = $1
	`, mapId).Scan(&mapName)
	if err != nil {
		return nil, err
	}
	return &mapName, nil
}

func CreateMatchDetails(db *sql.DB, match_id, round_id int, server_ip, port, password, mapStr string) error {
	query := `
	INSERT INTO matches_server_details (match_id, server_ip, port, password, map, round_id, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
	`
	_, err := db.Exec(query, match_id, server_ip, port, password, mapStr, round_id)
	if err != nil {
		return fmt.Errorf("failed to create match details: %w", err)
	}
	return nil
}

func DeleteMatchDetails(db *sql.DB, match_id, round_id int) error {
	query := `
	DELETE FROM matches_server_details
	WHERE match_id = $1 AND round_id = $2
	`
	_, err := db.Exec(query, match_id, round_id)
	if err != nil {
		return fmt.Errorf("failed to delete match details: %w", err)
	}
	return nil
}

func FetchMatchDetails(db *sql.DB, matchID, roundID int) (*MatchDetails, error) {
	query := `
	SELECT match_id, server_ip, port, password, map
	FROM matches_server_details
	WHERE match_id = $1 AND round_id = $2
	`
	var details MatchDetails
	err := db.QueryRow(query, matchID, roundID).Scan(&details.MatchID, &details.ServerIP, &details.Port, &details.Password, &details.Map)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &details, nil
}

func UpdateMatchRound(db *sql.DB, roundID, winnerID, loserID, homeTeamScore, awayTeamScore int) error {
	scoreDifference := float64(homeTeamScore - awayTeamScore)
	if scoreDifference < 0 {
		scoreDifference = -scoreDifference
	}

	query := `
	UPDATE league_match_rounds
	SET winner_id = $1, loser_id = $2, home_team_score = $3, away_team_score = $4, has_outcome = TRUE, score_difference = $5
	WHERE id = $6
	`

	_, err := db.Exec(query, winnerID, loserID, homeTeamScore, awayTeamScore, scoreDifference, roundID)
	if err != nil {
		return fmt.Errorf("failed to update match round: %w", err)
	}
	return nil
}

func UpdateMatchStatus(db *sql.DB, matchID int, status int) error {
	query := `
	UPDATE league_matches
	SET status = $1
	WHERE id = $2
	`

	_, err := db.Exec(query, status, matchID)
	if err != nil {
		return fmt.Errorf("failed to update match status: %w", err)
	}
	return nil
}

func CreateUserNotification(db *sql.DB, userID int, message, link string) error {
	query := `
	INSERT INTO user_notifications (user_id, read, message, link, created_at, updated_at)
	VALUES ($1, FALSE, $2, $3, NOW(), NOW())
	`
	_, err := db.Exec(query, userID, message, link)
	if err != nil {
		return fmt.Errorf("failed to create user notification: %w", err)
	}
	return nil
}

func SendNotificationsToTeams(db *sql.DB, homeRosterId, awayRosterId int, message, link string) error {
	homeUserIDs, err := FetchTeamUserIDs(db, homeRosterId)
	if err != nil {
		return fmt.Errorf("failed to fetch home team user IDs: %w", err)
	}

	awayUserIDs, err := FetchTeamUserIDs(db, awayRosterId)
	if err != nil {
		return fmt.Errorf("failed to fetch away team user IDs: %w", err)
	}

	for _, userID := range homeUserIDs {
		err := CreateUserNotification(db, userID, message, link)
		if err != nil {
			return fmt.Errorf("failed to send notification to home team user %d: %w", userID, err)
		}
	}

	for _, userID := range awayUserIDs {
		err := CreateUserNotification(db, userID, message, link)
		if err != nil {
			return fmt.Errorf("failed to send notification to away team user %d: %w", userID, err)
		}
	}

	return nil
}
