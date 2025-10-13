package templates

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"math/big"
	"os/exec"
	"strconv"
	"strings"
	"text/template"
)

type UdlServer struct {
	MatchID      string `json:"matchId" db:"match_id"`
	MatchRound   int    `json:"matchRound"`
	Division     string `json:"division" db:"division"`
	AwayTeamID   string `json:"awayTeamId" db:"away_team_id"`
	HomeTeamID   string `json:"homeAwayTeamId" db:"home_team_id"`
	WinLimit     int    `json:"winLimit" db:"win_limit"`
	AwayTeam     string `json:"awayTeam"`
	HomeTeam     string `json:"homeTeam"`
	MinPlayers   int    `json:"minPlayers"`
	MaxPlayers   int    `json:"maxPlayers"`
	SRCDSToken   string `json:"srcdsToken"`
	Password     string `json:"password"`
	Map          string `json:"map"`
	Port         int    `json:"port"`
	RCON         string `json:"rcon"`
	SourceTVPort int    `json:"sourceTVPort"`
	ClientPort   int    `json:"clientPort"`
	SteamPort    int    `json:"steamPort"`
}

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func generatePassword(length int) (string, error) {
	password := make([]byte, length)
	for i := range password {
		randomInt, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		password[i] = charset[randomInt.Int64()]
	}
	return string(password), nil
}

func getUsedPorts() (map[int]bool, error) {
	cmd := exec.Command("kubectl", "get", "pods", "-n", "udl", "-o", "jsonpath={.items[*].spec.containers[*].ports[*].containerPort}")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	ports := strings.Fields(string(output))
	usedPorts := make(map[int]bool)
	for _, port := range ports {
		p, err := strconv.Atoi(port)
		if err != nil {
			return nil, err
		}
		usedPorts[p] = true
	}
	return usedPorts, nil
}

func findFreePort(start, end int, usedPorts map[int]bool) (int, error) {
	for port := start; port <= end; port++ {
		if !usedPorts[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free ports available in the range %d-%d", start, end)
}

func NewUdlServer(matchID, division, awayTeamID, homeTeamID, awayTeam, homeTeam string, matchRound, minPlayers, maxPlayers int) (*UdlServer, error) {
	password, err := generatePassword(10)
	if err != nil {
		return nil, fmt.Errorf("failed to create a password: %w", err)
	}

	return &UdlServer{
		MatchID:    matchID,
		Division:   division,
		AwayTeamID: awayTeamID,
		HomeTeamID: homeTeamID,
		AwayTeam:   awayTeam,
		HomeTeam:   homeTeam,
		Password:   password,
		MatchRound: matchRound,
		MinPlayers: minPlayers,
		MaxPlayers: maxPlayers,
	}, nil
}

func (u *UdlServer) GetName() string {
	return fmt.Sprintf("udl-%s-%d", u.MatchID, u.MatchRound)
}

func (u *UdlServer) SetWinLimit(winLimit int) {
	u.WinLimit = winLimit
}

func (u *UdlServer) SetSRCDSToken(token string) {
	u.SRCDSToken = token
}

func (u *UdlServer) SetMap(mapStr string) {
	u.Map = mapStr
}

func (u *UdlServer) RenderTemplate() (string, error) {

	usedPorts, err := getUsedPorts()
	if err != nil {
		return "", fmt.Errorf("failed to get used ports: %w", err)
	}

	port, err := findFreePort(30015, 30100, usedPorts)
	if err != nil {
		return "", fmt.Errorf("failed to find a free port: %w", err)
	}
	u.Port = port

	sourceTVPort, err := findFreePort(30101, 30200, usedPorts)
	if err != nil {
		return "", fmt.Errorf("failed to find a free SourceTV port: %w", err)
	}
	u.SourceTVPort = sourceTVPort

	clientPort, err := findFreePort(30201, 30300, usedPorts)
	if err != nil {
		return "", fmt.Errorf("failed to find a free client port: %w", err)
	}
	u.ClientPort = clientPort

	steamPort, err := findFreePort(30301, 30400, usedPorts)
	if err != nil {
		return "", fmt.Errorf("failed to find a free steam port: %w", err)
	}
	u.SteamPort = steamPort

	rconPassword, err := generatePassword(46)
	if err != nil {
		return "", fmt.Errorf("failed to find a free port: %w", err)
	}

	u.RCON = rconPassword

	tmpl, err := template.New("udl_server").Parse(UDL_SERVER)
	if err != nil {
		return "", err
	}

	var renderedTemplate bytes.Buffer
	err = tmpl.Execute(&renderedTemplate, u)
	if err != nil {
		return "", err
	}

	return renderedTemplate.String(), nil
}

const UDL_SERVER = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: udl-{{ .MatchID }}-{{ .MatchRound }}
  namespace: udl
  labels:
    app: udl
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: udl
  template:
    metadata:
      labels:
        app: udl
    spec:
      containers:
        - name: udl-{{ .MatchID }}-{{ .MatchRound }}
          image: 'ghcr.io/dodgeball-tf/tf2:sourcemod'
          imagePullPolicy: Always
          stdin: true
          tty: true
          ports:
            - containerPort: {{ .Port }}
              protocol: UDP
            - containerPort: {{ .Port }}
              protocol: TCP
            - containerPort: {{ .SourceTVPort }}
              protocol: UDP
            - containerPort: {{ .ClientPort }}
              protocol: UDP
            - containerPort: {{ .SteamPort }}
              protocol: UDP
          env:
            - name: SRCDS_PORT
              value: "{{ .Port }}"
            - name: SRCDS_PW
              value: "{{ .Password }}"
            - name: SRCDS_MAXPLAYERS
              value: "12"
            - name: SRCDS_TICKRATE
              value: '128'
            - name: SRCDS_RCONPW
              value: "{{ .RCON }}"
            - name: SRCDS_STARTMAP
              value: {{ or .Map "tfdb_octagon_odb_a1" }}
            - name: SRCDS_STATIC_HOSTNAME
              value: "UDL.TF | {{ .MatchID }} | Round #{{ .MatchRound }}"
            - name: SRCDS_TOKEN
              value: {{ .SRCDSToken }}
            - name: SRCDS_TV_PORT
              value: "{{ .SourceTVPort }}"
            - name: SRCDS_CLIENT_PORT
              value: "{{ .ClientPort }}"
            - name: SRCDS_STEAM_PORT
              value: "{{ .SteamPort }}"
            - name: MATCH_ID
              value: "{{ .MatchID }}"
            - name: ROUND_ID
              value: "{{ .MatchRound }}"
            - name: AWAY_TEAM
              value: "{{ .AwayTeam }}"
            - name: AWAY_TEAM_ID
              value: "{{ .AwayTeamID }}"
            - name: HOME_TEAM
              value: "{{ .HomeTeam }}"
            - name: HOME_TEAM_ID
              value: "{{ .HomeTeamID }}"
            - name: MIN_PLAYERS
              value: "{{ .MinPlayers }}"
            - name: MAX_PLAYERS
              value: "{{ .MaxPlayers }}"
            - name: WIN_LIMIT
              value: "{{ .WinLimit }}"
          volumeMounts:
            - mountPath: /home/steam/tf-dedicated/
              name: tf-dedicated
      tolerations:
        - key: "key"
          operator: "Equal"
          value: "value"
          effect: "NoSchedule"
      hostNetwork: true
      volumes:
        - name: tf-dedicated
          hostPath:
            path: /tf/udl
            type: ''
---
apiVersion: v1
kind: Service
metadata:
  name: udl-{{ .MatchID }}-{{ .MatchRound }}
  namespace: udl
spec:
  selector:
    app: udl
  ports:
    - name: game-udp
      protocol: UDP
      port: {{ .Port }}
      targetPort: {{ .Port }}
			nodePort: {{ .Port }}
    - name: game-tcp
      protocol: TCP
      port: {{ .Port }}
      targetPort: {{ .Port }}
			nodePort: {{ .Port }}
    - name: sourcetv
      protocol: UDP
      port: {{ .SourceTVPort }}
      targetPort: {{ .SourceTVPort }}
			nodePort: {{ .SourceTVPort }}
    - name: clientport
      protocol: UDP
      port: {{ .ClientPort }}
      targetPort: {{ .ClientPort }}
			nodePort: {{ .ClientPort }}
    - name: steamport
      protocol: UDP
      port: {{ .SteamPort }}
      targetPort: {{ .SteamPort }}
			nodePort: {{ .SteamPort }}
  type: NodePort
`
