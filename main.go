package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	USERNAME     = "shv-ng"
	BASE_URL     = "https://api.github.com"
	STAR_URL     = "https://api.github-star-counter.workers.dev"
	FILE_NAME    = "terminal.svg"
	MAX_BIO_LEN  = 45
	MAX_LANG_LEN = 35
)

// SVG structure definitions
type SVG struct {
	XMLName    xml.Name `xml:"svg"`
	Xmlns      string   `xml:"xmlns,attr"`
	Width      string   `xml:"width,attr"`
	Height     string   `xml:"height,attr"`
	ViewBox    string   `xml:"viewBox,attr"`
	PreserveAR string   `xml:"preserveAspectRatio,attr"`
	Background Rect     `xml:"rect"`
	Texts      []Text   `xml:"text"`
	Style      Style    `xml:"style"`
}

type Rect struct {
	ID     string `xml:"id,attr"`
	Class  string `xml:"class,attr"`
	Width  string `xml:"width,attr"`
	Height string `xml:"height,attr"`
	RX     string `xml:"rx,attr"`
	RY     string `xml:"ry,attr"`
	X      string `xml:"x,attr"`
	Y      string `xml:"y,attr"`
}

type Text struct {
	ID    string  `xml:"id,attr"`
	Class string  `xml:"class,attr"`
	X     string  `xml:"x,attr"`
	Y     string  `xml:"y,attr"`
	Value string  `xml:",chardata"`
	Tspan []Tspan `xml:"tspan,omitempty"`
}

type Tspan struct {
	ID    string `xml:"id,attr,omitempty"`
	Class string `xml:"class,attr,omitempty"`
	X     string `xml:"x,attr,omitempty"`
	DY    string `xml:"dy,attr,omitempty"`
	Value string `xml:",chardata"`
}

type Style struct {
	Value string `xml:",cdata"`
}

// GitHub API response structures
type GitHubUser struct {
	Login       string `json:"login"`
	Followers   int    `json:"followers"`
	Following   int    `json:"following"`
	Bio         string `json:"bio"`
	PublicRepos int    `json:"public_repos"`
}

type GitHubRepo struct {
	Name       string `json:"name"`
	Language   string `json:"language"`
	CommitsURL string `json:"commits_url"`
	Fork       bool   `json:"fork"`
	Archived   bool   `json:"archived"`
}

type StarResponse struct {
	Stars int `json:"stars"`
}

type GitHubStats struct {
	User              *GitHubUser
	Repos             []GitHubRepo
	Stars             int
	TotalCommits      int
	LanguageCount     map[string]int
	MostUsedLanguages string
}

// APIManager handles all GitHub API interactions
type APIManager struct {
	client *http.Client
	stats  *GitHubStats
}

func NewAPIManager() *APIManager {
	return &APIManager{
		client: &http.Client{Timeout: 30 * time.Second},
		stats: &GitHubStats{
			LanguageCount: make(map[string]int),
		},
	}
}

func (api *APIManager) fetchJSON(url string, target interface{}) error {
	resp, err := api.client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API request failed with status: %d for URL: %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	return json.Unmarshal(body, target)
}

func (api *APIManager) fetchUserData() error {
	url := fmt.Sprintf("%s/users/%s", BASE_URL, USERNAME)
	api.stats.User = &GitHubUser{}
	return api.fetchJSON(url, api.stats.User)
}

func (api *APIManager) fetchStarCount() error {
	url := fmt.Sprintf("%s/user/%s", STAR_URL, USERNAME)
	starResp := &StarResponse{}
	err := api.fetchJSON(url, starResp)
	if err != nil {
		return err
	}
	api.stats.Stars = starResp.Stars
	return nil
}

func (api *APIManager) fetchRepos() error {
	url := fmt.Sprintf("%s/users/%s/repos?per_page=100", BASE_URL, USERNAME)
	return api.fetchJSON(url, &api.stats.Repos)
}

func (api *APIManager) countCommits() error {
	totalCommits := 0
	excludedLanguages := map[string]bool{
		"HTML": true, "Jupyter Notebook": true, "Brainfuck": true,
	}

	for _, repo := range api.stats.Repos {
		if repo.Fork || repo.Archived {
			continue
		}

		if repo.Language != "" && !excludedLanguages[repo.Language] {
			api.stats.LanguageCount[repo.Language]++
		}

		commitsURL := strings.Replace(repo.CommitsURL, "{/sha}", "", 1)
		commitsURL += "?per_page=100"

		var commits []map[string]interface{}
		if err := api.fetchJSON(commitsURL, &commits); err != nil {
			log.Printf("Warning: Could not fetch commits for repo %s: %v", repo.Name, err)
			continue
		}
		totalCommits += len(commits)
	}

	api.stats.TotalCommits = totalCommits
	api.generateMostUsedLanguages()
	return nil
}

func (api *APIManager) generateMostUsedLanguages() {
	type langCount struct {
		lang  string
		count int
	}

	var langCounts []langCount
	for lang, count := range api.stats.LanguageCount {
		langCounts = append(langCounts, langCount{lang, count})
	}

	sort.Slice(langCounts, func(i, j int) bool {
		return langCounts[i].count > langCounts[j].count
	})

	var result strings.Builder
	totalLen := 0

	for i, lc := range langCounts {
		langLen := len(lc.lang)
		if i > 0 {
			langLen += 2
		}

		if totalLen+langLen > MAX_LANG_LEN {
			break
		}

		if i > 0 {
			result.WriteString(", ")
		}
		result.WriteString(lc.lang)
		totalLen += langLen
	}

	api.stats.MostUsedLanguages = result.String()
}

func (api *APIManager) Setup() error {
	log.Println("Fetching user data...")
	if err := api.fetchUserData(); err != nil {
		return fmt.Errorf("failed to fetch user data: %w", err)
	}

	log.Println("Fetching star count...")
	if err := api.fetchStarCount(); err != nil {
		return fmt.Errorf("failed to fetch star count: %w", err)
	}

	log.Println("Fetching repositories...")
	if err := api.fetchRepos(); err != nil {
		return fmt.Errorf("failed to fetch repos: %w", err)
	}

	log.Println("Counting commits and analyzing languages...")
	if err := api.countCommits(); err != nil {
		return fmt.Errorf("failed to count commits: %w", err)
	}

	return nil
}

func (api *APIManager) GetBio() string {
	bio := api.stats.User.Bio
	if bio == "" {
		bio = "New user"
	}
	if len(bio) > MAX_BIO_LEN {
		return bio[:MAX_BIO_LEN] + "..."
	}
	return bio
}

// SVG Generator
type SVGGenerator struct {
	api *APIManager
}

func NewSVGGenerator(api *APIManager) *SVGGenerator {
	return &SVGGenerator{api: api}
}

func (sg *SVGGenerator) generateAsciiArt() []Tspan {
	artLines := []string{
		"⠀⠀⠀⠀⠀⠀⠀⢀⣠⣤⣤⣶⣶⣶⣶⣤⣤⣄⡀⠀⠀⠀⠀⠀⠀⠀",
		"⠀⠀⠀⠀⢀⣤⣾⣿⣿⠿⠟⠛⠛⠛⠛⠻⠿⣿⣿⣷⣤⡀⠀⠀⠀⠀",
		"⠀⠀⠀⣴⣿⣿⠟⠋⠁⠀⠀⠀⠀⠀⠀⠀⠀⠈⠙⠻⣿⣿⣦⠀⠀⠀",
		"⠀⢀⣾⣿⡿⠁⠀⠀⣴⣦⣄⠀⠀⠀⠀⠀⣀⣤⣶⡀⠈⢿⣿⣷⡀⠀",
		"⠀⣾⣿⡟⠁⠀⠀⠀⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⠃⠀⠈⢻⣿⣷⠀",
		"⢠⣿⣿⠁⠀⠀⠀⣠⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣦⠀⠀⠈⣿⣿⡄",
		"⢸⣿⣿⠀⠀⠀⢰⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⡇⠀⠀⣿⣿⡇",
		"⠘⣿⣿⡦⠤⠒⠒⢿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⣿⡿⠧⠤⢴⣿⣿⠃",
		"⠀⢿⣿⣧⡀⠀⢤⡀⠙⠻⠿⣿⣿⣿⣿⣿⡿⠟⠋⠁⠀⢀⣼⣿⡿⠀",
		"⠀⠈⢿⣿⣷⡀⠈⢿⣦⣤⣾⣿⣿⣿⣿⣿⣷⣄⠀⠀⢀⣾⣿⡿⠁⠀",
		"⠀⠀⠀⠻⣿⣿⣦⣄⡉⣿⣿⢿⣿⠉⢻⣿⢿⣿⣠⣴⣿⣿⠟⠀⠀⠀",
		"⠀⠀⠀⠀⠈⠛⢿⣿⣿⣿⣧⣼⣿⣤⣾⣷⣶⣿⣿⡿⠛⠁⠀⠀⠀⠀",
		"⠀⠀⠀⠀⠀⠀⠀⠈⠙⠛⠛⠿⠿⠿⠿⠛⠛⠋⠁⠀⠀⠀⠀⠀⠀⠀",
	}

	var tspans []Tspan
	for _, line := range artLines {
		tspans = append(tspans, Tspan{
			X:     "30",
			DY:    "1.3em",
			Value: line,
		})
	}
	return tspans
}

func (sg *SVGGenerator) Generate() *SVG {
	currentTime := time.Now().Format("Mon Jan 02 15:04:05 2006 on tty1")

	svg := &SVG{
		Xmlns:      "http://www.w3.org/2000/svg",
		Width:      "1040",
		Height:     "660",
		ViewBox:    "0 0 1020 650",
		PreserveAR: "xMidYMid",
		Background: Rect{
			ID:     "bg-rect",
			Class:  "bg",
			Width:  "1000",
			Height: "620",
			RX:     "20",
			RY:     "20",
			X:      "10",
			Y:      "10",
		},
		Texts: []Text{
			{
				ID:    "text-1",
				Class: "text",
				X:     "30",
				Y:     "40",
				Value: "Arch Linux 6.7.1-arch1-1 (tty1)",
			},
			{
				ID:    "text-2",
				Class: "text",
				X:     "30",
				Y:     "80",
				Value: "github.com login: ",
				Tspan: []Tspan{
					{
						ID:    "login-username",
						Class: "login",
						Value: USERNAME,
					},
				},
			},
			{
				ID:    "text-3",
				Class: "text",
				X:     "30",
				Y:     "110",
				Value: "password: ",
				Tspan: []Tspan{
					{
						ID:    "password",
						Class: "password",
						Value: "******",
					},
				},
			},
			{
				ID:    "text-4",
				Class: "text",
				X:     "30",
				Y:     "140",
				Value: "Last login: ",
				Tspan: []Tspan{
					{
						ID:    "last-login",
						Class: "last-login",
						Value: currentTime,
					},
				},
			},
			{
				ID:    "text-5",
				Class: "text",
				X:     "30",
				Y:     "190",
				Value: "[" + USERNAME + "@github ~]$ ",
				Tspan: []Tspan{
					{
						ID:    "whoami",
						Class: "command",
						Value: "./whoami.sh",
					},
				},
			},
			{
				ID:    "art",
				Class: "art",
				X:     "30",
				Y:     "220",
				Tspan: sg.generateAsciiArt(),
			},
			{
				ID:    "profile-info",
				Class: "profile",
				X:     "400",
				Y:     "220",
				Tspan: []Tspan{
					{
						ID:    "profile-username",
						X:     "400",
						DY:    "1.3em",
						Value: USERNAME,
					},
					{
						ID:    "profile-separator",
						X:     "400",
						DY:    "1.3em",
						Value: "-----------------------",
					},
					{
						ID:    "user-bio",
						X:     "400",
						DY:    "2.3em",
						Value: "Bio: " + sg.api.GetBio(),
					},
					{
						ID:    "followers",
						X:     "400",
						DY:    "1.3em",
						Value: fmt.Sprintf("Followers: %d", sg.api.stats.User.Followers),
					},
					{
						ID:    "profile-following",
						X:     "400",
						DY:    "1.3em",
						Value: fmt.Sprintf("Following: %d", sg.api.stats.User.Following),
					},
					{
						ID:    "total-repo",
						X:     "400",
						DY:    "2.3em",
						Value: fmt.Sprintf("Total Repo: %d", sg.api.stats.User.PublicRepos),
					},
					{
						ID:    "total-stars",
						X:     "400",
						DY:    "1.3em",
						Value: fmt.Sprintf("Total Stars: %d", sg.api.stats.Stars),
					},
					{
						ID:    "total-commits",
						X:     "400",
						DY:    "1.3em",
						Value: fmt.Sprintf("Total Commits: %d", sg.api.stats.TotalCommits),
					},
					{
						ID:    "most-used-language",
						X:     "400",
						DY:    "1.3em",
						Value: "Most used language: " + sg.api.stats.MostUsedLanguages,
					},
				},
			},
			{
				ID:    "reboot-message",
				Class: "text",
				X:     "30",
				Y:     "550",
				Value: "[" + USERNAME + "@github ~]$ ",
				Tspan: []Tspan{
					{
						ID:    "reboot-command",
						Class: "reboot-command",
						Value: `echo "Reboot in 5 sec..." ; sleep 5 ; reboot`,
					},
					{
						ID:    "reboot-status",
						X:     "30",
						DY:    "2em",
						Value: "Reboot in 5 sec...",
					},
				},
			},
		},
		Style: Style{
			Value: `
        * {
            font-family: 'JetBrains Mono', monospace;
        }

        .bg {
            fill: #11111b;
            filter: drop-shadow(5px 5px 10px rgba(0, 0, 0, 0.5));
        }

        #text-1 {
            fill: #f38ba8;
        }

        #text-2,
        #text-3 {
            fill: #f5c2e7;
        }

        .text {
            font-size: 17px;
            fill: #cdd6f4;
        }

        .text tspan {
            fill: #9399b2;
        }

        .command {
            fill: #a6e3a1 !important;
        }

        .str-command {
            fill: #fab387 !important;
        }

        .art {
            font-size: 15px;
            fill: #89b4fa;
        }

        .profile {
            font-size: 17px;
            fill: #89dceb;
        }
        
        #reboot-command, #reboot-status {
            display: none !important;
        }
            `,
		},
	}

	return svg
}

func (sg *SVGGenerator) SaveToFile(filename string) error {
	svg := sg.Generate()

	output, err := xml.MarshalIndent(svg, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal SVG: %w", err)
	}

	// Add XML declaration
	xmlContent := `<?xml version="1.0" ?>` + string(output)

	return os.WriteFile(filename, []byte(xmlContent), 0644)
}

func main() {
	log.Println("Starting GitHub Profile README Generator...")

	// Initialize API manager and fetch data
	apiManager := NewAPIManager()
	if err := apiManager.Setup(); err != nil {
		log.Fatal("Failed to setup API manager:", err)
	}

	log.Println("Generating SVG...")

	// Generate and save SVG
	svgGenerator := NewSVGGenerator(apiManager)
	if err := svgGenerator.SaveToFile(FILE_NAME); err != nil {
		log.Fatal("Failed to generate SVG file:", err)
	}

	log.Println("Successfully generated", FILE_NAME)

	// Print summary
	fmt.Println("\n=== GitHub Profile Stats ===")
	fmt.Printf("Followers: %d\n", apiManager.stats.User.Followers)
	fmt.Printf("Following: %d\n", apiManager.stats.User.Following)
	fmt.Printf("Public Repos: %d\n", apiManager.stats.User.PublicRepos)
	fmt.Printf("Total Stars: %d\n", apiManager.stats.Stars)
	fmt.Printf("Total Commits: %d\n", apiManager.stats.TotalCommits)
	fmt.Printf("Most Used Languages: %s\n", apiManager.stats.MostUsedLanguages)
	fmt.Printf("Bio: %s\n", apiManager.GetBio())
}
