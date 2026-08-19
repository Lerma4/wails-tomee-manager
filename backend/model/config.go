package model

type Config struct {
	TomEEPath    string `json:"tomeePath"`
	JavaHome     string `json:"javaHome"`
	HTTPPort     int    `json:"httpPort"`
	DebugPort    int    `json:"debugPort"`
	ShutdownPort int    `json:"shutdownPort"`
	// VMOptions is passed to the JVM as CATALINA_OPTS, e.g.
	// "-Xmx2g -Dspring.profiles.active=dev".
	VMOptions string `json:"vmOptions"`
	// OpenBrowser opens the app root once the server reports startup.
	OpenBrowser bool `json:"openBrowser"`
	// IsolatedBase runs the server against a private CATALINA_BASE instead of
	// writing into the TomEE installation. Off by default: turning it on gives
	// the server an empty webapps directory until things are deployed again.
	IsolatedBase bool `json:"isolatedBase"`
}

// Deploy modes for a WarArtifact.
const (
	// DeployCopy copies the built .war into webapps/. Tomcat then unpacks it.
	DeployCopy = "copy"
	// DeployWar points a context descriptor at the built .war in place.
	DeployWar = "war"
	// DeployExploded points a context descriptor at the exploded directory
	// Maven writes next to the .war, so a rebuild is picked up without copying.
	DeployExploded = "exploded"
)

type WarArtifact struct {
	ID         int    `json:"id"`
	SourcePath string `json:"sourcePath"`
	// DestName is the context path the app is served on, e.g. "/commerciale".
	// The built artifact keeps its own name; only under webapps/ do the two
	// have to agree, because Tomcat derives the context from the file name
	// there.
	DestName   string `json:"destName"`
	Enabled    bool   `json:"enabled"`
	DeployMode string `json:"deployMode"`
	// DeployedAs is the context this artifact was last deployed under, so a
	// changed context path can clean up after itself. Set by the deploy, not
	// by the user.
	DeployedAs string `json:"deployedAs"`
}
