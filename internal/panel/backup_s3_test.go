package panel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// Публичные значения из примера AWS: сверяем с независимо опубликованным
// результатом, а не с подписью, полученной вторым вызовом своего кода.
func TestS3SignaturesMatchThePublishedAWSExamples(t *testing.T) {
	cfg := S3BackupSettings{Region: "us-east-1", AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}
	now := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	for _, example := range []struct {
		name, method, url, body, signature string
		headers                            map[string]string
	}{
		{"чтение", "GET", "https://examplebucket.s3.amazonaws.com/test.txt", "", "f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41", map[string]string{"Range": "bytes=0-9"}},
		{"загрузка", "PUT", "https://examplebucket.s3.amazonaws.com/test%24file.text", "Welcome to Amazon S3.", "98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd", map[string]string{"Date": "Fri, 24 May 2013 00:00:00 GMT", "X-Amz-Storage-Class": "REDUCED_REDUNDANCY"}},
		{"список", "GET", "https://examplebucket.s3.amazonaws.com/?max-keys=2&prefix=J", "", "34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7", nil},
	} {
		t.Run(example.name, func(t *testing.T) {
			req, err := http.NewRequest(example.method, example.url, strings.NewReader(example.body))
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range example.headers {
				req.Header.Set(k, v)
			}
			signS3Request(req, []byte(example.body), cfg, now)
			if !strings.HasSuffix(req.Header.Get("Authorization"), "Signature="+example.signature) {
				t.Fatalf("подпись разошлась с AWS: %s", req.Header.Get("Authorization"))
			}
		})
	}
}

func TestS3KeepsTheLatestCopiesAndLeavesOtherObjectsAlone(t *testing.T) {
	s, dir := scheduledStore(t)
	ctx := context.Background()
	objects := map[string][]byte{"копии +/заметки.txt": []byte("чужой файл"), "другая-панель/panel-2026-09-01-030000.000000000.db.sealed": []byte("чужая копия")}
	pages := 0
	failUpload := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		hash := sha256.Sum256(body)
		if r.Header.Get("X-Amz-Content-Sha256") != hex.EncodeToString(hash[:]) || !strings.Contains(r.Header.Get("Authorization"), "/s3/aws4_request") || r.Header.Get("X-Amz-Security-Token") != "временная-сессия" {
			t.Error("S3 получил неподписанный запрос")
		}
		key := strings.TrimPrefix(r.URL.Path, "/backups/")
		switch r.Method {
		case http.MethodPut:
			if failUpload {
				http.Error(w, "недоступно", http.StatusServiceUnavailable)
				return
			}
			if !strings.Contains(r.URL.EscapedPath(), "%20%2B") {
				t.Error("префикс S3 испортился при кодировании")
			}
			objects[key] = body
		case http.MethodGet:
			pages++
			if r.URL.Query().Get("list-type") != "2" || r.URL.Query().Get("prefix") != "копии +/" {
				t.Error("список запрошен без префикса")
			}
			var keys []string
			for key := range objects {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			// Каждая выдача разбита на две страницы; токен содержит знаки,
			// на которых QueryEscape без поправки на SigV4 дал бы другой запрос.
			token := r.URL.Query().Get("continuation-token")
			truncated := token == ""
			if truncated {
				keys = keys[:1]
			} else {
				if token != "страница +/=" {
					t.Error("токен страницы изменился")
				}
				keys = keys[1:]
			}
			fmt.Fprintf(w, "<ListBucketResult><IsTruncated>%t</IsTruncated><NextContinuationToken>страница +/=</NextContinuationToken>", truncated)
			for _, key := range keys {
				_, _ = io.WriteString(w, "<Contents><Key>")
				_ = xml.EscapeText(w, []byte(key))
				_, _ = io.WriteString(w, "</Key></Contents>")
			}
			_, _ = io.WriteString(w, "</ListBucketResult>")
		case http.MethodDelete:
			if !strings.HasPrefix(key, "копии +/panel-") {
				t.Error("удаляется чужой объект")
			}
			delete(objects, key)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	cfg := scheduledConfig()
	cfg.Telegram = false
	cfg.S3 = S3BackupSettings{Enabled: true, Endpoint: server.URL, Region: "us-east-1", Bucket: "backups", Prefix: "копии +/", AccessKey: "идентификатор", SecretKey: "секрет", SessionToken: "временная-сессия"}
	if err := s.SetBackupSettings(ctx, cfg, scheduledTestPassword); err != nil {
		t.Fatal(err)
	}
	b := NewScheduledBackups(s, nil, filepath.Join(dir, "scheduled"))
	b.s3Client = server.Client()
	for day := 5; day <= 8; day++ {
		if err := b.Check(ctx, time.Date(2026, 10, day, 4, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
	}
	if len(objects) != 4 || pages != 8 {
		t.Fatalf("ротация или пагинация потеряла копии: объектов %d, страниц %d", len(objects), pages)
	}
	for key, body := range objects {
		if strings.HasPrefix(key, "копии +/panel-") {
			if !strings.Contains(key, "2026-10-07") && !strings.Contains(key, "2026-10-08") {
				t.Fatalf("S3 сохранил старую копию: %s", key)
			}
			if _, err := OpenPasswordBackup(body, scheduledTestPassword); err != nil {
				t.Fatalf("копия из S3 не открывается: %v", err)
			}
		}
	}
	failUpload = true
	if err := b.Check(ctx, time.Date(2026, 10, 9, 4, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("отказ PUT остался незамечен")
	}
	if len(objects) != 4 || pages != 8 {
		t.Fatal("после отказа загрузки панель чистила годные копии")
	}
}

func TestS3FailureDoesNotPreventTelegramDeliveryAndRaisesAnAlert(t *testing.T) {
	s, dir := scheduledStore(t)
	ctx := context.Background()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "не работает", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	cfg := scheduledConfig()
	cfg.S3 = S3BackupSettings{Enabled: true, Endpoint: server.URL, Region: "test", Bucket: "backups", Prefix: "panel/", AccessKey: "ключ", SecretKey: "секрет"}
	if err := s.SetBackupSettings(ctx, cfg, scheduledTestPassword); err != nil {
		t.Fatal(err)
	}
	alerts := NewAlerts(s)
	var text string
	alerts.SendWith(func(_ context.Context, _ AlertSettings, message string) error { text = message; return nil })
	b := NewScheduledBackups(s, alerts, filepath.Join(dir, "scheduled"))
	b.s3Client = server.Client()
	delivered := 0
	b.sendDocument = func(context.Context, AlertSettings, string, int64) (int64, error) { delivered++; return 1, nil }
	if err := b.RunNow(ctx); err == nil || delivered != 1 || !strings.Contains(text, "S3") {
		t.Fatalf("частичный сбой скрыт или Telegram не получил копию: %v, %q", err, text)
	}
}

func TestAnOpenSnapshotIsRejectedBeforeS3MakesAnyRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel-2026-10-05-030000.000000000.db.sealed")
	if err := os.WriteFile(path, []byte("SQLite format 3"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := S3BackupSettings{Endpoint: "https://example.com", Region: "test", Bucket: "backups", Prefix: "panel/", AccessKey: "ключ", SecretKey: "секрет"}
	if err := cfg.uploadAndPrune(context.Background(), nil, path, 2); err == nil {
		t.Fatal("открытая копия дошла до отправки")
	}
}
