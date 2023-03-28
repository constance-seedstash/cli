package nhost

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/nhost/cli/util"
	"gopkg.in/yaml.v2"
)

func RunCmdAndCaptureStderrIfNotSetup(cmd *exec.Cmd) error {
	var errBuf *bytes.Buffer

	if cmd.Stderr == nil {
		errBuf = bytes.NewBuffer(nil)
		cmd.Stderr = errBuf
	}

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("%s\n%s", err, errBuf.String())
	}

	return nil
}

func (r *Project) MarshalYAML() ([]byte, error) {
	return yaml.Marshal(r)
}

func (r *Project) MarshalJSON() ([]byte, error) {
	return json.Marshal(r)
}

func (r *Configuration) MarshalYAML() ([]byte, error) {
	return yaml.Marshal(r)
}

func (r *Configuration) MarshalJSON() ([]byte, error) {
	return json.Marshal(r)
}

func InitLocations() error {

	//	if required directories don't exist, then create them
	for _, dir := range LOCATIONS.Directories {
		if err := os.MkdirAll(*dir, os.ModePerm); err != nil {
			return err
		}
		log.Debug("Created ", util.Rel(*dir))
	}

	//	#106: Don't create file if it already exists.
	//	Otherwise, it will reset the contents of the file.
	for _, file := range LOCATIONS.Files {
		if !util.PathExists(*file) {
			if _, err := os.Create(*file); err != nil {
				return err
			}
			log.Debug("Created ", util.Rel(*file))
		} else {
			log.Debug("Found existing ", util.Rel(*file))
		}
	}

	return nil
}

func (config *Configuration) Save() error {

	log.Debug("Saving app configuration")

	//  convert generated Nhost configuration to YAML
	marshalled, err := config.MarshalYAML()
	if err != nil {
		return err
	}

	f, err := os.Create(CONFIG_PATH)
	if err != nil {
		return err
	}

	defer f.Close()

	//  write the marshalled YAML configuration to file
	if _, err = f.Write(marshalled); err != nil {
		return err
	}

	f.Sync()

	return nil
}

func Info() (App, error) {
	log.Debug("Fetching app information")

	var response App

	file, err := os.ReadFile(INFO_PATH)
	if err != nil {
		return response, err
	}

	err = json.Unmarshal(file, &response)
	return response, err
}

// fetches the required asset from release
// depending on OS and Architecture
// by matching download URL
func (release *Release) Asset() Asset {

	log.Debug("Extracting asset from release")

	payload := []string{"cli", release.TagName, runtime.GOOS, runtime.GOARCH}

	var response Asset

	for _, asset := range release.Assets {
		if strings.Contains(asset.BrowserDownloadURL, strings.Join(payload, "-")) {
			response = asset
			break
		}
	}

	return response
}

func (r *Release) MarshalJSON() ([]byte, error) {
	return json.Marshal(r)
}

// Compares and updates the changelog for specified release
func (r *Release) Changes(releases []Release) (string, error) {

	var response string
	for _, item := range releases {
		item_time, _ := time.Parse(time.RFC3339, item.CreatedAt)
		release_time, _ := time.Parse(time.RFC3339, r.CreatedAt)

		//	If the release is older,
		//	update changelog
		if item_time.After(release_time) {
			response += item.Body
		}
	}

	return response, nil
}

// Seaches for required release from supplied list of releases, and returns it.
func SearchRelease(releases []Release, version string) (Release, error) {

	log.Debug("Fetching latest release")

	var response Release

	//	If a custom version has been passed,
	//	search for that one ONLY.
	//	Otherwise, search for the latest release.
	//	If no release is found, return an error.
	//	If a release is found, return it.
	if version != "" {
		for _, item := range releases {
			if item.TagName == strings.ToLower(version) {
				return item, nil
			}
		}
		return response, errors.New("no such release found")

	} else {

		//	If no custom version has been passed,
		//	search for the latest release.
		//	If there are no releases, return an error.
		//	If there are releases, return the latest one.
		//	If there are multiple releases, return the latest one.

		//	Following loop is used under the assumption,
		//	that GitHub's API will always return release list,
		//	in descending order of timestamps.
		//	That is, the latest release being on index 0.
		for _, item := range releases {

			//	Else, search for latest release fit for public use.
			//	Following code has been commented because we are shifting
			//	from "internal" releases to pre-releases.
			/*
						if !strings.Contains(item.TagName, "internal") {
						   				return item, nil
				   			}
			*/

			//	Skip the pre-releases.
			if !item.Prerelease {
				return item, nil
			}
		}
	}

	if len(releases) == 0 {
		return response, errors.New("no release found")
	}

	return releases[0], nil
}

// Downloads the list of all releases from GitHub API
func GetReleases() ([]Release, error) {

	log.Debug("Fetching list of all releases")

	var array []Release

	resp, err := http.Get("https://cli.nhost.io/releases.json")
	if err != nil {
		return array, err
	}

	//  read our opened xmlFile as a byte array.
	body, _ := ioutil.ReadAll(resp.Body)
	defer resp.Body.Close()
	json.Unmarshal(body, &array)
	return array, nil
}

// fetches the list of Nhost production servers
func Servers() ([]Server, error) {

	log.Debug("Fetching server locations")

	var response []Server

	resp, err := http.Get(API + "/custom/cli/get-server-locations")
	if err != nil {
		return response, err
	}

	//  read our opened xmlFile as a byte array.
	body, _ := ioutil.ReadAll(resp.Body)

	defer resp.Body.Close()

	var res map[string]interface{}
	//  we unmarshal our body byteArray which contains our
	//  jsonFile's content into 'server' strcuture
	err = json.Unmarshal(body, &res)
	if err != nil {
		return response, err
	}

	locations, err := json.Marshal(res["server_locations"])
	if err != nil {
		return response, err
	}

	err = json.Unmarshal(locations, &response)
	return response, err
}

// fetches saved credentials from auth file
func LoadCredentials() (*Credentials, error) {
	log.Debug("Fetching saved auth credentials")

	//  we initialize our credentials array
	var credentials Credentials

	if util.PathExists(AUTH_PATH) == false {
		return nil, fmt.Errorf("auth file does not exist, run 'nhost login' first")
	}
	//  Open our jsonFile
	jsonFile, err := os.Open(AUTH_PATH)
	//  if we os.Open returns an error then handle it
	if err != nil {
		return nil, err
	}

	//  defer the closing of our jsonFile so that we can parse it later on
	defer jsonFile.Close()

	//  read our opened xmlFile as a byte array.
	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		return nil, err
	}

	//  we unmarshal our byteArray which contains our
	//  jsonFile's content into 'credentials' which we defined above
	err = json.Unmarshal(byteValue, &credentials)

	return &credentials, err
}

func GetUser(creds *Credentials) (*User, error) {
	postBody, err := json.Marshal(creds)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal credentials: %w", err)
	}

	resp, err := http.Post(API+"/custom/cli/user", "application/json", bytes.NewBuffer(postBody))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user: %w", err)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	defer resp.Body.Close()

	var response User

	if err = json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response body: %w:\n%s", err, string(body))
	}

	if response.ID == "" {
		err = errors.New("user not found")
	}

	return &response, err
}
