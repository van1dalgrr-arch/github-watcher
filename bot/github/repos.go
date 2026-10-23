package github

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type Repository struct {
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	Description string `json:"description"`
}

func GetRepos(username string) ([]Repository, error) {
	url := fmt.Sprintf("https://api.github.com/users/%s/repos", username)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("get repositories: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get repositories: unexpected HTTP status: %d", resp.StatusCode)
	}

	var repos []Repository

	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, fmt.Errorf("decode repositories: %w", err)
	}

	return repos, nil
}
