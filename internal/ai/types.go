package ai

type Response struct {
	Intent     string  `json:"intent"`
	Command    string  `json:"command"`
	Confidence float64 `json:"confidence"`
	Risk       string  `json:"risk"`
}

type ollamaGenerateRequest struct {
	Model   string                 `json:"model"`
	Prompt  string                 `json:"prompt"`
	Stream  bool                   `json:"stream"`
	Format  string                 `json:"format,omitempty"`
	Options map[string]interface{} `json:"options,omitempty"`
}

type ollamaGenerateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}
