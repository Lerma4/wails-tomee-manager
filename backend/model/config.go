package model

type Config struct {
	TomEEPath    string `json:"tomeePath"`
	JavaHome     string `json:"javaHome"`
	HTTPPort     int    `json:"httpPort"`
	DebugPort    int    `json:"debugPort"`
	ShutdownPort int    `json:"shutdownPort"`
}

type WarArtifact struct {
	ID         int    `json:"id"`
	SourcePath string `json:"sourcePath"`
	DestName   string `json:"destName"`
	Enabled    bool   `json:"enabled"`
}
