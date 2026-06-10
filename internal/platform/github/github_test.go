package github

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient points a Client at a test server for both the API and OAuth base URLs.
func newTestClient(ts *httptest.Server) *Client {
	return NewClient(Config{APIBaseURL: ts.URL, OAuthBaseURL: ts.URL, HTTPClient: ts.Client()})
}

func TestGetRepository_SuccessAndHeaders(t *testing.T) {
	var gotAuth, gotAccept, gotVersion, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotVersion = r.Header.Get("X-GitHub-Api-Version")
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{
			"name": "example", "full_name": "octocat/example", "default_branch": "main",
			"private": true, "html_url": "https://github.com/octocat/example",
			"description": "hi", "owner": {"login": "octocat"}
		}`))
	}))
	defer ts.Close()

	repo, err := newTestClient(ts).GetRepository(context.Background(), "tok", "octocat", "example")
	if err != nil {
		t.Fatalf("GetRepository: %v", err)
	}
	if repo.Owner != "octocat" || repo.Name != "example" || repo.FullName != "octocat/example" {
		t.Fatalf("unexpected identity: %+v", repo)
	}
	if repo.DefaultBranch != "main" || !repo.Private || repo.HTMLURL == "" {
		t.Fatalf("unexpected metadata: %+v", repo)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer tok")
	}
	if gotAccept != acceptJSON {
		t.Errorf("Accept = %q, want %q", gotAccept, acceptJSON)
	}
	if gotVersion != apiVersion {
		t.Errorf("X-GitHub-Api-Version = %q, want %q", gotVersion, apiVersion)
	}
	if gotPath != "/repos/octocat/example" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestGetRepository_NoTokenOmitsAuthHeader(t *testing.T) {
	var hadAuth bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		_, _ = w.Write([]byte(`{"name":"x","owner":{"login":"y"}}`))
	}))
	defer ts.Close()

	if _, err := newTestClient(ts).GetRepository(context.Background(), "", "y", "x"); err != nil {
		t.Fatalf("GetRepository: %v", err)
	}
	if hadAuth {
		t.Error("Authorization header should be absent when token is empty")
	}
}

func TestStatusMapping(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		headers map[string]string
		want    error
	}{
		{"not found", http.StatusNotFound, nil, ErrNotFound},
		{"unauthorized", http.StatusUnauthorized, nil, ErrUnauthorized},
		{"rate limited via 403", http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "0"}, ErrRateLimited},
		{"forbidden", http.StatusForbidden, nil, ErrForbidden},
		{"too many requests", http.StatusTooManyRequests, nil, ErrRateLimited},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
			}))
			defer ts.Close()

			_, err := newTestClient(ts).GetRepository(context.Background(), "tok", "o", "r")
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestGetRepository_UnexpectedStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	_, err := newTestClient(ts).GetRepository(context.Background(), "tok", "o", "r")
	if err == nil {
		t.Fatal("expected an error for a 500")
	}
	// A 5xx must not be misclassified as one of the precise sentinels.
	for _, s := range []error{ErrNotFound, ErrUnauthorized, ErrForbidden, ErrRateLimited} {
		if errors.Is(err, s) {
			t.Fatalf("500 mapped to sentinel %v", s)
		}
	}
}

func TestExchangeCode_Success(t *testing.T) {
	var gotForm, gotAccept string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		_ = r.ParseForm()
		gotForm = r.Form.Get("code")
		_, _ = w.Write([]byte(`{"access_token": "gho_abc", "scope": "repo"}`))
	}))
	defer ts.Close()

	tok, err := newTestClient(ts).ExchangeCode(context.Background(), "cid", "csec", "the-code", "https://app/cb")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "gho_abc" || tok.Scope != "repo" {
		t.Fatalf("unexpected token: %+v", tok)
	}
	if gotForm != "the-code" {
		t.Errorf("code form value = %q", gotForm)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
}

func TestExchangeCode_ErrorBodyIs200(t *testing.T) {
	// GitHub returns 200 with an error field for e.g. bad_verification_code.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error":"bad_verification_code","error_description":"The code is incorrect"}`))
	}))
	defer ts.Close()

	_, err := newTestClient(ts).ExchangeCode(context.Background(), "cid", "csec", "bad", "https://app/cb")
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("got %v, want ErrUnauthorized", err)
	}
}

func TestListBranches(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("per_page") != "100" {
			t.Errorf("per_page = %q, want 100", r.URL.Query().Get("per_page"))
		}
		_, _ = w.Write([]byte(`[{"name":"main"},{"name":"dev"}]`))
	}))
	defer ts.Close()

	branches, err := newTestClient(ts).ListBranches(context.Background(), "tok", "o", "r")
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if len(branches) != 2 || branches[0] != "main" || branches[1] != "dev" {
		t.Fatalf("unexpected branches: %v", branches)
	}
}

func TestListUserRepos_QueryAndMapping(t *testing.T) {
	var gotSort, gotPerPage, gotPage string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSort = r.URL.Query().Get("sort")
		gotPerPage = r.URL.Query().Get("per_page")
		gotPage = r.URL.Query().Get("page")
		_, _ = w.Write([]byte(`[{"name":"a","full_name":"o/a","owner":{"login":"o"}}]`))
	}))
	defer ts.Close()

	repos, err := newTestClient(ts).ListUserRepos(context.Background(), "tok", ListReposOptions{Sort: "updated"})
	if err != nil {
		t.Fatalf("ListUserRepos: %v", err)
	}
	if len(repos) != 1 || repos[0].FullName != "o/a" || repos[0].Owner != "o" {
		t.Fatalf("unexpected repos: %+v", repos)
	}
	if gotSort != "updated" || gotPerPage != "100" || gotPage != "1" {
		t.Errorf("query: sort=%q per_page=%q page=%q", gotSort, gotPerPage, gotPage)
	}
}

func TestGetAuthenticatedUser(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Errorf("path = %q, want /user", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"login":"octocat","id":583231}`))
	}))
	defer ts.Close()

	u, err := newTestClient(ts).GetAuthenticatedUser(context.Background(), "tok")
	if err != nil {
		t.Fatalf("GetAuthenticatedUser: %v", err)
	}
	if u.Login != "octocat" || u.ID != 583231 {
		t.Fatalf("unexpected user: %+v", u)
	}
}

func TestAuthorizeURL(t *testing.T) {
	c := NewClient(Config{OAuthBaseURL: "https://github.com"})
	got := c.AuthorizeURL("cid", "https://app/cb", "repo", "xyz")
	for _, want := range []string{
		"https://github.com/login/oauth/authorize?",
		"client_id=cid",
		"state=xyz",
		"scope=repo",
		"redirect_uri=https%3A%2F%2Fapp%2Fcb",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("AuthorizeURL %q missing %q", got, want)
		}
	}
}

func TestGetBranch(t *testing.T) {
	t.Run("exists", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/repos/o/r/branches/main" {
				t.Errorf("path = %q", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"name":"main"}`))
		}))
		defer ts.Close()
		if err := newTestClient(ts).GetBranch(context.Background(), "tok", "o", "r", "main"); err != nil {
			t.Fatalf("GetBranch: %v", err)
		}
	})
	t.Run("missing maps to ErrNotFound", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()
		if err := newTestClient(ts).GetBranch(context.Background(), "tok", "o", "r", "nope"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("got %v, want ErrNotFound", err)
		}
	})
}

func TestRevokeToken(t *testing.T) {
	t.Run("success sends basic auth, DELETE, and the token", func(t *testing.T) {
		var gotUser, gotPass, gotMethod, gotPath, gotBody string
		var hadBasic bool
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotUser, gotPass, hadBasic = r.BasicAuth()
			gotMethod = r.Method
			gotPath = r.URL.Path
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.WriteHeader(http.StatusNoContent)
		}))
		defer ts.Close()
		if err := newTestClient(ts).RevokeToken(context.Background(), "cid", "csec", "gho_x"); err != nil {
			t.Fatalf("RevokeToken: %v", err)
		}
		if !hadBasic || gotUser != "cid" || gotPass != "csec" {
			t.Errorf("basic auth = (%q,%q,%v), want (cid,csec,true)", gotUser, gotPass, hadBasic)
		}
		if gotMethod != http.MethodDelete || gotPath != "/applications/cid/token" {
			t.Errorf("request = %s %s, want DELETE /applications/cid/token", gotMethod, gotPath)
		}
		if !strings.Contains(gotBody, "gho_x") {
			t.Errorf("body = %q, want it to carry the token", gotBody)
		}
	})
	t.Run("404 and 422 are treated as success", func(t *testing.T) {
		for _, status := range []int{http.StatusNotFound, http.StatusUnprocessableEntity} {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			if err := newTestClient(ts).RevokeToken(context.Background(), "cid", "csec", "t"); err != nil {
				t.Errorf("status %d: got %v, want nil", status, err)
			}
			ts.Close()
		}
	})
	t.Run("server error is returned", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()
		if err := newTestClient(ts).RevokeToken(context.Background(), "cid", "csec", "t"); err == nil {
			t.Error("expected an error for a 500")
		}
	})
}
