package workflow

import (
	"embed"
	"fmt"
	"strings"
	"time"
)

//go:embed roles/*.txt
var roleFiles embed.FS

func roleTemplate(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		role = "audit"
	}
	data, err := roleFiles.ReadFile("roles/" + role + ".txt")
	if err != nil {
		return fmt.Sprintf("You are the %s subagent in a dynamic workflow. Follow the prompt exactly and return only the requested result.", role)
	}
	return strings.ReplaceAll(string(data), "{{today}}", time.Now().Format("2006-01-02"))
}
