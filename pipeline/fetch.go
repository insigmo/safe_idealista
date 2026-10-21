package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

const (
	ckanPackageShow = "https://opendata-ajuntament.barcelona.cat/data/api/3/action/package_show?id="
	socrataBase     = "https://analisi.transparenciacatalunya.cat/resource/"
)

// Source describes one downloaded input; it is copied into grid.json metadata.
type Source struct {
	Portal   string `json:"portal"`
	Dataset  string `json:"dataset"`
	Title    string `json:"title"`
	Resource string `json:"resource"`
	URL      string `json:"url"`
	Modified string `json:"modified,omitempty"`
}

type fetcher struct {
	dir     string
	refresh bool
	client  *http.Client
}

func newFetcher(dir string, refresh bool) (*fetcher, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &fetcher{dir: dir, refresh: refresh, client: &http.Client{Timeout: 10 * time.Minute}}, nil
}

// get downloads rawURL into the cache as name, reusing an existing copy unless refresh is set.
func (f *fetcher) get(rawURL, name string) (string, error) {
	path := filepath.Join(f.dir, name)
	if st, err := os.Stat(path); err == nil && st.Size() > 0 && !f.refresh {
		return path, nil
	}
	logf("download %s", rawURL)
	resp, err := f.client.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	tmp := path + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return path, os.Rename(tmp, path)
}

type ckanResource struct {
	Name   string `json:"name"`
	Format string `json:"format"`
	URL    string `json:"url"`
}

type ckanPackage struct {
	Name      string         `json:"name"`
	Title     string         `json:"title"`
	Modified  string         `json:"metadata_modified"`
	Resources []ckanResource `json:"resources"`
}

// ckanPackage always re-fetches package metadata (small) so that new yearly
// resources are picked up; the large resource files themselves are cached.
func (f *fetcher) ckanPackage(id string) (*ckanPackage, error) {
	resp, err := f.client.Get(ckanPackageShow + url.QueryEscape(id))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Success bool        `json:"success"`
		Result  ckanPackage `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("package_show %s: %w", id, err)
	}
	if !body.Success {
		return nil, fmt.Errorf("package_show %s: not found", id)
	}
	return &body.Result, nil
}

type yearResource struct {
	Year int
	Res  ckanResource
}

// yearlyCSV returns the CSV resources whose name matches re (first group = year), newest first.
func yearlyCSV(pkg *ckanPackage, re *regexp.Regexp) []yearResource {
	var out []yearResource
	for _, r := range pkg.Resources {
		m := re.FindStringSubmatch(r.Name)
		if m == nil {
			continue
		}
		var y int
		fmt.Sscanf(m[1], "%d", &y)
		out = append(out, yearResource{y, r})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Year > out[j].Year })
	return out
}

func (f *fetcher) ckanFile(pkg *ckanPackage, r ckanResource, cacheName string) (string, Source, error) {
	p, err := f.get(r.URL, cacheName)
	src := Source{Portal: "Open Data BCN", Dataset: pkg.Name, Title: pkg.Title, Resource: r.Name, URL: r.URL, Modified: pkg.Modified}
	return p, src, err
}

// socrata runs a SoQL query against a dataset on analisi.transparenciacatalunya.cat and caches the JSON.
func (f *fetcher) socrata(dataset string, params url.Values, cacheName string) (string, Source, error) {
	u := socrataBase + dataset + ".json?" + params.Encode()
	p, err := f.get(u, cacheName)
	src := Source{Portal: "Dades obertes de Catalunya (Socrata)", Dataset: dataset, URL: u}
	return p, src, err
}
