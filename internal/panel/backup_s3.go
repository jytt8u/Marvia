package panel

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Здесь три операции S3: PUT, ListObjectsV2 с пагинацией и DELETE. SDK ради
// них принёс бы отдельное дерево зависимостей; SigV4 следует документации AWS
// и использует только готовые HMAC-SHA256 из стандартной библиотеки.
// https://docs.aws.amazon.com/AmazonS3/latest/developerguide/sig-v4-header-based-auth.html
type S3BackupSettings struct {
	Enabled      bool   `json:"enabled"`
	Endpoint     string `json:"endpoint"`
	Region       string `json:"region"`
	Bucket       string `json:"bucket"`
	Prefix       string `json:"prefix"`
	AccessKey    string `json:"access_key"`
	SecretKey    string `json:"secret_key,omitempty"`
	SessionToken string `json:"session_token,omitempty"`
}

func (c S3BackupSettings) validate() error {
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("адрес S3 нужен как https://хост без пути, пароля и параметров")
	}
	if c.Region == "" || c.AccessKey == "" || c.SecretKey == "" {
		return errors.New("для S3 нужны регион, идентификатор и секретный ключ")
	}
	if strings.ContainsAny(c.Region+c.AccessKey+c.SecretKey+c.SessionToken, "\r\n") {
		return errors.New("ключи и регион S3 не должны содержать перенос строки")
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`).MatchString(c.Bucket) {
		return errors.New("имя бакета S3 должно содержать от 3 до 63 букв, цифр, точек или дефисов")
	}
	if c.Prefix == "" || strings.HasPrefix(c.Prefix, "/") || !strings.HasSuffix(c.Prefix, "/") || strings.Contains(c.Prefix, "..") || strings.ContainsAny(c.Prefix, "\\\r\n") {
		return errors.New("для копий S3 нужен отдельный префикс без .., например marvia-panel/")
	}
	return nil
}

var scheduledBackupName = regexp.MustCompile(`^panel-\d{4}-\d{2}-\d{2}-\d{6}\.\d{9}\.db\.sealed$`)

func (c S3BackupSettings) uploadAndPrune(ctx context.Context, client *http.Client, path string, keep int) error {
	if err := c.validate(); err != nil {
		return err
	}
	if keep < 1 {
		return errors.New("в S3 должна оставаться хотя бы одна копия")
	}
	name := filepath.Base(path)
	if !scheduledBackupName.MatchString(name) {
		return errors.New("имя копии S3 не распознано")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return errors.New("не удалось прочитать копию для S3")
	}
	if !bytes.HasPrefix(body, passwordBackupMagic) {
		return errors.New("открытая копия в S3 не отправляется")
	}
	resp, err := c.request(ctx, client, http.MethodPut, c.Prefix+name, nil, body)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	// Чистим только после подтверждённой загрузки. Иначе временный отказ PUT
	// удалил бы последние годные копии, ради которых хранилище и подключили.
	var keys []string
	continuation := ""
	seen := make(map[string]bool)
	for {
		query := url.Values{"list-type": {"2"}, "prefix": {c.Prefix}, "max-keys": {"1000"}}
		if continuation != "" {
			query.Set("continuation-token", continuation)
		}
		resp, err := c.request(ctx, client, http.MethodGet, "", query, nil)
		if err != nil {
			return fmt.Errorf("список копий: %w", err)
		}
		var page struct {
			XMLName   xml.Name `xml:"ListBucketResult"`
			Truncated bool     `xml:"IsTruncated"`
			Next      string   `xml:"NextContinuationToken"`
			Contents  []struct {
				Key string `xml:"Key"`
			} `xml:"Contents"`
		}
		err = xml.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&page)
		_ = resp.Body.Close()
		if err != nil {
			return errors.New("S3 вернул непонятный список копий")
		}
		for _, object := range page.Contents {
			if strings.HasPrefix(object.Key, c.Prefix) && scheduledBackupName.MatchString(strings.TrimPrefix(object.Key, c.Prefix)) {
				keys = append(keys, object.Key)
			}
		}
		if !page.Truncated {
			break
		}
		if page.Next == "" || seen[page.Next] {
			return errors.New("S3 не продвигается по страницам списка копий")
		}
		seen[page.Next], continuation = true, page.Next
	}
	sort.Strings(keys)
	if len(keys) <= keep {
		return nil
	}
	for _, key := range keys[:len(keys)-keep] {
		resp, err := c.request(ctx, client, http.MethodDelete, key, nil, nil)
		if err != nil {
			return fmt.Errorf("удаление старой копии: %w", err)
		}
		_ = resp.Body.Close()
	}
	return nil
}

func (c S3BackupSettings) request(ctx context.Context, client *http.Client, method, key string, query url.Values, body []byte) (*http.Response, error) {
	u, err := url.Parse(c.Endpoint)
	if err != nil {
		return nil, errors.New("адрес S3 повреждён")
	}
	u.Path = "/" + c.Bucket
	if key != "" {
		u.Path += "/" + key
	}
	u.RawPath = awsPath(u.Path)
	u.RawQuery = strings.ReplaceAll(query.Encode(), "+", "%20")
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("не удалось подготовить запрос S3")
	}
	signS3Request(req, body, c, time.Now().UTC())
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("хранилище S3 недоступно")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		// Тело ответа и URL не цитируем: прокси и провайдер могут отразить ключи.
		return nil, fmt.Errorf("S3 отклонил %s (код %d)", method, resp.StatusCode)
	}
	return resp, nil
}

func awsPath(path string) string {
	parts := strings.Split(path, "/")
	for i := range parts {
		parts[i] = strings.ReplaceAll(url.QueryEscape(parts[i]), "+", "%20")
	}
	return strings.Join(parts, "/")
}

func signS3Request(req *http.Request, body []byte, cfg S3BackupSettings, now time.Time) {
	day, stamp := now.UTC().Format("20060102"), now.UTC().Format("20060102T150405Z")
	hash := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(hash[:])
	req.Header.Set("X-Amz-Date", stamp)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if cfg.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", cfg.SessionToken)
	}
	// Включаем все переданные заголовки: официальный пример AWS использует
	// Range, а временные ключи требуют подписи x-amz-security-token.
	headers := map[string]string{"host": req.URL.Host}
	for k, values := range req.Header {
		if strings.EqualFold(k, "Authorization") {
			continue
		}
		headers[strings.ToLower(k)] = strings.Join(strings.Fields(strings.Join(values, ",")), " ")
	}
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonicalHeaders strings.Builder
	for _, k := range names {
		canonicalHeaders.WriteString(k + ":" + headers[k] + "\n")
	}
	signed := strings.Join(names, ";")
	canonical := req.Method + "\n" + req.URL.EscapedPath() + "\n" + req.URL.RawQuery + "\n" + canonicalHeaders.String() + "\n" + signed + "\n" + payloadHash
	canonicalHash := sha256.Sum256([]byte(canonical))
	scope := day + "/" + cfg.Region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + stamp + "\n" + scope + "\n" + hex.EncodeToString(canonicalHash[:])
	dateKey := s3HMAC([]byte("AWS4"+cfg.SecretKey), day)
	regionKey := s3HMAC(dateKey, cfg.Region)
	serviceKey := s3HMAC(regionKey, "s3")
	key := s3HMAC(serviceKey, "aws4_request")
	signature := hex.EncodeToString(s3HMAC(key, toSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+cfg.AccessKey+"/"+scope+", SignedHeaders="+signed+", Signature="+signature)
}

func s3HMAC(key []byte, value string) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(value))
	return h.Sum(nil)
}
