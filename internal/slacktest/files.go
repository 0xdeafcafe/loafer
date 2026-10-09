package slacktest

import (
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/jsonx"
)

// file is one upload: asked for, then its bytes put, then shared.
type file struct {
	name string
	size int
	data []byte
	put  bool
}

// File is the bytes uploaded under name, if any were.
func (s *Server) File(name string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.files {
		if f.name == name && f.put {
			return f.data, true
		}
	}
	return nil, false
}

// uploads serves the two API calls either side of the bytes. Call with the
// lock held.
func (s *Server) uploads(method string, f url.Values) (map[string]any, string) {
	if method == "files.getUploadURLExternal" {
		n, err := strconv.Atoi(f.Get("length"))
		if f.Get("filename") == "" || err != nil || n <= 0 {
			return nil, "invalid_arguments"
		}
		id := "F" + strconv.Itoa(len(s.files)+1)
		s.files[id] = &file{name: f.Get("filename"), size: n}
		return map[string]any{"upload_url": s.URL + "/upload/" + id, "file_id": id}, ""
	}

	c := s.convs[f.Get("channel_id")]
	if c == nil {
		return nil, "channel_not_found"
	}
	var list []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if jsonx.Unmarshal([]byte(f.Get("files")), &list) != nil || len(list) == 0 {
		return nil, "invalid_arguments"
	}
	var shared []map[string]any
	for _, x := range list {
		up := s.files[x.ID]
		if up == nil || !up.put {
			return nil, "invalid_file_id"
		}
		kind := mime.TypeByExtension(path.Ext(up.name))
		if kind == "" {
			kind = "application/octet-stream"
		}
		link := s.URL + "/files/" + x.ID + "/" + url.PathEscape(up.name)
		shared = append(shared, map[string]any{"id": x.ID, "name": up.name, "title": x.Title, "mimetype": kind,
			"size": up.size, "mode": "hosted", "permalink": link, "url_private_download": link})
	}
	raw, err := jsonx.Marshal(shared)
	if err != nil {
		panic(err) // maps of strings and ints: a bug here, not bad input
	}
	m := slack.Message{Type: "message", Subtype: "file_share", TS: s.ts(), User: Self,
		Text: f.Get("initial_comment"), ThreadTS: f.Get("thread_ts"), Files: raw}
	s.add(c, m)
	s.pushMsg(c.ID, m)
	return map[string]any{"files": list}, ""
}

// upload takes a file's bytes at the URL getUploadURLExternal gave.
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	up := s.files[strings.TrimPrefix(r.URL.Path, "/upload/")]
	if err != nil || up == nil || len(b) != up.size {
		http.Error(w, "bad upload", http.StatusBadRequest)
		return
	}
	up.data, up.put = b, true
	_, _ = w.Write([]byte("OK - " + strconv.Itoa(len(b))))
}

// download serves an uploaded file back, at /files/<id>/<name>.
func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	id, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/files/"), "/")
	s.mu.Lock()
	defer s.mu.Unlock()
	if up := s.files[id]; up != nil && up.put {
		_, _ = w.Write(up.data)
		return
	}
	http.NotFound(w, r)
}
