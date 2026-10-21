package translator

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

func sanitizeToolArgs(toolName, argsJSON string) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args == nil {
		// Argumen kosong, "null", atau bukan objek JSON tidak boleh diteruskan
		// apa adanya: "null" menghasilkan tool_use tanpa input dan upstream
		// Claude (Vertex) menolak seluruh request dengan 400
		// "messages.N.content.X.tool_use.input: Field required".
		return "{}"
	}

	name := toolName
	if strings.HasPrefix(name, "proxy_") {
		name = strings.TrimPrefix(name, "proxy_")
	}

	if name == "Read" {
		sanitizeReadArgs(args)
	}

	sanitized, err := json.Marshal(args)
	if err != nil {
		return argsJSON
	}
	return string(sanitized)
}

// normalizeToolArgs mengubah string argumen tool-call menjadi map yang tak
// pernah nil. json.Unmarshal terhadap literal "null" BERHASIL tanpa error
// namun menyisakan map nil; karena GeminiFunctionCall.Args tidak ber-omitempty,
// nilai nil itu keluar sebagai "args":null dan upstream Claude (Vertex)
// menolaknya dengan 400 "messages.N.content.X.tool_use.input: Field required".
func normalizeToolArgs(argsJSON string) map[string]any {
	if strings.TrimSpace(argsJSON) == "" {
		return map[string]any{}
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args == nil {
		return map[string]any{}
	}
	return args
}

// marshalToolArgs menyerialkan argumen tool-call dengan jaminan hasilnya objek
// JSON: map nil (mis. functionCall dari upstream yang tak membawa args) menjadi
// {} — bukan null.
func marshalToolArgs(args map[string]any) []byte {
	if args == nil {
		args = map[string]any{}
	}
	out, err := json.Marshal(args)
	if err != nil {
		return []byte("{}")
	}
	return out
}

func sanitizeReadArgs(args map[string]any) {
	if limitVal, ok := args["limit"]; ok {
		switch v := limitVal.(type) {
		case string:
			var n int
			if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
				args["limit"] = n
			}
		}
		if limitNum, ok := args["limit"].(float64); ok {
			n := int(limitNum)
			if n > 2000 {
				args["limit"] = 2000
			} else if n < 1 {
				delete(args, "limit")
			} else {
				args["limit"] = n
			}
		} else if limitNum, ok := args["limit"].(int); ok {
			if limitNum > 2000 {
				args["limit"] = 2000
			} else if limitNum < 1 {
				delete(args, "limit")
			}
		}
	}

	if offsetVal, ok := args["offset"]; ok {
		switch v := offsetVal.(type) {
		case string:
			var n int
			if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
				args["offset"] = n
			}
		}
		if offsetNum, ok := args["offset"].(float64); ok {
			n := int(offsetNum)
			if n < 0 {
				args["offset"] = 0
			} else {
				args["offset"] = n
			}
		} else if offsetNum, ok := args["offset"].(int); ok {
			if offsetNum < 0 {
				args["offset"] = 0
			}
		}
	}

	if pagesVal, ok := args["pages"]; ok {
		filePath, _ := args["file_path"].(string)
		pages, _ := pagesVal.(string)
		if !isValidPdfPagesArg(filePath, pages) {
			delete(args, "pages")
		}
	}
}

func isValidPdfPagesArg(filePath, pages string) bool {
	if filePath == "" || pages == "" {
		return false
	}
	filePathLower := strings.ToLower(filePath)
	if !strings.HasSuffix(filePathLower, ".pdf") {
		return false
	}
	matched, _ := regexp.MatchString(`^\d+(-\d+)?$`, pages)
	return matched
}
