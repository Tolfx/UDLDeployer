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
	MatchID    string `json:"matchId" db:"match_id"`
	MatchRound int    `json:"matchRound"`
	Division   string `json:"division" db:"division"`
	AwayTeamID string `json:"awayTeamId" db:"away_team_id"`
	HomeTeamID string `json:"homeAwayTeamId" db:"home_team_id"`
	AwayTeam   string `json:"awayTeam"`
	HomeTeam   string `json:"homeTeam"`
	SRCDSToken string `json:"srcdsToken"`
	Password   string `json:"password"`
	Map        string `json:"map"`
	Port       int    `json:"port"`
	RCON       string `json:"rcon"`
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
	cmd := exec.Command("kubectl", "get", "pods", "--all-namespaces", "-o", "jsonpath={.items[*].spec.containers[*].ports[*].containerPort}")
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

func findFreePort(start, end int) (int, error) {
	usedPorts, err := getUsedPorts()
	if err != nil {
		return 0, err
	}

	for port := start; port <= end; port++ {
		if !usedPorts[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free ports available in the range %d-%d", start, end)
}

func NewUdlServer(matchID, division, awayTeamID, homeTeamID, awayTeam, homeTeam string, matchRound int) (*UdlServer, error) {
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
	}, nil
}

func (u *UdlServer) GetName() string {
	return fmt.Sprintf("udl-%s-%d", u.MatchID, u.MatchRound)
}

func (u *UdlServer) SetSRCDSToken(token string) {
	u.SRCDSToken = token
}

func (u *UdlServer) SetMap(mapStr string) {
	u.Map = mapStr
}

func (u *UdlServer) RenderTemplate() (string, error) {

	port, err := findFreePort(30015, 30100)
	if err != nil {
		return "", fmt.Errorf("failed to find a free port: %w", err)
	}

	u.Port = port

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
          env:
            - name: SRCDS_PORT
              value: "{{ .Port }}"
            - name: SRCDS_PW
              value: "{{ .Password }}"
            - name: SRCDS_MAXPLAYERS
              value: "12"
            - name: SRCDS_RCONPW
              value: "{{ .RCON }}"
            - name: SRCDS_STARTMAP
              value: {{ or .Map "tfdb_octagon_odb_a1" }}
            - name: SRCDS_STATIC_HOSTNAME
              value: UDL.TF | {{ .Division }} | Match #{{ .MatchID }}
            - name: SRCDS_TOKEN
              value: {{ .SRCDSToken }}
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
          volumeMounts:
            - mountPath: /home/steam/tf-dedicated/
              name: tf-dedicated
          securityContext:
            runAsUser: 0
            runAsGroup: 1000
      tolerations:
        - key: "key"
          operator: "Equal"
          value: "value"
          effect: "NoSchedule"
      hostNetwork: true
      volumes:
        - name: tf-dedicated
          hostPath:
            path: /home/udl
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
    - protocol: UDP
      port: {{ .Port }}
      targetPort: {{ .Port }}
  type: NodePort
`
