package secrets

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const fileHeader = "BULLSECRETSv1\n"

func ResolvePath(configFile, secretsFile string) (string, error) {
	if strings.TrimSpace(secretsFile) == "" {
		return "", fmt.Errorf("secrets file path must not be empty")
	}
	if filepath.IsAbs(secretsFile) {
		return secretsFile, nil
	}
	absConfig, err := filepath.Abs(configFile)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(absConfig), secretsFile), nil
}

func Load(path, key string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, fmt.Errorf("secrets file %q exists but no key provided (set --secret-key or BULL_SECRET_KEY)", path)
	}

	plain, err := decrypt(string(data), key)
	if err != nil {
		return nil, fmt.Errorf("decrypt secrets: %w", err)
	}
	return parseEnv(plain)
}

func Save(path, key string, values map[string]string) error {
	if key == "" {
		return fmt.Errorf("missing encryption key")
	}
	if values == nil {
		values = map[string]string{}
	}

	content := renderEnv(values)
	enc, err := encrypt(content, key)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(enc), 0o600)
}

func Edit(path, key string) error {
	if key == "" {
		return fmt.Errorf("missing encryption key")
	}

	values, err := Load(path, key)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp("", "bull-secrets-*.env")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(renderEnv(values)); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("open editor %q: %w", editor, err)
	}

	edited, err := os.ReadFile(tmpPath)
	if err != nil {
		return err
	}
	newValues, err := parseEnv(string(edited))
	if err != nil {
		return err
	}
	return Save(path, key, newValues)
}

func encrypt(plaintext, key string) (string, error) {
	cipherKey := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(cipherKey[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	payload := append(nonce, ciphertext...)
	return fileHeader + base64.StdEncoding.EncodeToString(payload) + "\n", nil
}

func decrypt(fileContent, key string) (string, error) {
	if !strings.HasPrefix(fileContent, fileHeader) {
		return "", fmt.Errorf("invalid secrets file header")
	}

	encoded := strings.TrimSpace(strings.TrimPrefix(fileContent, fileHeader))
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}

	cipherKey := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(cipherKey[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid payload")
	}

	nonce := payload[:gcm.NonceSize()]
	ciphertext := payload[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func parseEnv(content string) (map[string]string, error) {
	values := map[string]string{}
	s := bufio.NewScanner(strings.NewReader(content))
	lineNo := 0
	for s.Scan() {
		lineNo++
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid line %d: expected KEY=value", lineNo)
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" {
			return nil, fmt.Errorf("invalid line %d: empty key", lineNo)
		}
		values[k] = v
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func renderEnv(values map[string]string) string {
	if len(values) == 0 {
		return "# KEY=value\n"
	}

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(values[k])
		b.WriteString("\n")
	}
	return b.String()
}
