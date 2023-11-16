// Copyright 2023 Dmitry Dikun
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"io"
	"log"
	"net/http"
	"os"
)

func downloadDriveFile(fs *drive.FilesService, src, dst string) (string, error) {
	return fetchDriveFile(fs, src, "", dst, "")
}

func exportDriveFile(fs *drive.FilesService, src, srcMIME, dst, dstMIME string) (string, error) {
	return fetchDriveFile(fs, src, srcMIME, dst, dstMIME)
}

func fetchDriveFile(fs *drive.FilesService, src, srcMIME, dst, dstMIME string) (string, error) {
	id, err := getDriveFileId(fs, src, srcMIME)
	if err != nil {
		return "", err
	}
	rc, err := getDriveFileReadCloser(fs, id, dstMIME)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, filePerm)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err = io.Copy(f, rc); err != nil {
		return "", err
	}
	return id, nil
}

func getDriveFileId(fs *drive.FilesService, src, mime string) (string, error) {
	q := "name = '" + src + "'"
	if mime != "" {
		q += "and mimeType = '" + mime + "'"
	}
	list, err := fs.List().Q(q).Do()
	if err != nil {
		return "", err
	}
	if len(list.Files) != 1 {
		if len(list.Files) != 0 {
			log.Printf("failed to find file %s, candidates:\n", src)
			for _, f := range list.Files {
				log.Printf("%s\t%s\n", f.Id, f.Name)
			}
		}
		return "", errors.New("file not found")
	}
	return list.Files[0].Id, nil
}

func getDriveFileReadCloser(fs *drive.FilesService, id string, mime string) (io.ReadCloser, error) {
	var r *http.Response
	var err error
	if mime != "" {
		r, err = fs.Export(id, mime).Download()
	} else {
		r, err = fs.Get(id).Download()
	}
	if err != nil {
		return nil, err
	}
	return r.Body, nil
}

func getDriveFilesService(cfg *config) (*drive.FilesService, error) {
	ctx := context.Background()
	b, err := os.ReadFile(cfg.GoogleCredentialsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read client secret file: %v", err)
	}

	// If modifying these scopes, delete your previously saved token.json.
	auth, err := google.ConfigFromJSON(b, drive.DriveScope)
	if err != nil {
		return nil, fmt.Errorf("failed to parse client secret file to config: %v", err)
	}
	client, err := getClient(auth, cfg.GoogleTokenFile)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize client: %v", err)
	}

	srv, err := drive.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %v", err)
	}
	return srv.Files, nil
}

// Retrieve a token, saves the token, then returns the generated client.
func getClient(auth *oauth2.Config, file string) (*http.Client, error) {
	// The file token.json stores the user's access and refresh tokens, and is
	// created automatically when the authorization flow completes for the first
	// time.
	tok, err := tokenFromFile(file)
	if err != nil {
		if tok, err = getTokenFromWeb(auth); err != nil {
			return nil, err
		}
		if err = saveToken(file, tok); err != nil {
			return nil, err
		}
	}
	return auth.Client(context.Background(), tok), nil
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(config *oauth2.Config) (*oauth2.Token, error) {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		return nil, fmt.Errorf("failed to read authorization code: %v", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("failed to retrieve token: %v", err)
	}
	return tok, nil
}

// Retrieves a token from a local file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// Saves a token to a file path.
func saveToken(path string, token *oauth2.Token) error {
	log.Printf("saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to save credential file: %v", err)
	}
	defer f.Close()
	if err = json.NewEncoder(f).Encode(token); err != nil {
		return fmt.Errorf("failed to encode credential file: %v", err)
	}
	return nil
}
