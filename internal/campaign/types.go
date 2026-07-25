package campaign

const (
	APIVersionV1Alpha1 = "cutline.dev/v1alpha1"
	KindCampaign       = "Campaign"
)

type Campaign struct {
	APIVersion  string          `yaml:"apiVersion" json:"apiVersion"`
	Kind        string          `yaml:"kind" json:"kind"`
	Name        string          `yaml:"name" json:"name"`
	Target      TargetSpec      `yaml:"target" json:"target"`
	Exploration ExplorationSpec `yaml:"exploration" json:"exploration"`
	Contracts   []ContractSpec  `yaml:"contracts" json:"contracts"`
	Output      OutputSpec      `yaml:"output,omitempty" json:"output"`
}

type TargetSpec struct {
	Adapter          string   `yaml:"adapter" json:"adapter"`
	Command          []string `yaml:"command" json:"command"`
	WorkingDirectory string   `yaml:"workingDirectory,omitempty" json:"workingDirectory,omitempty"`
}

type ExplorationSpec struct {
	Strategy        string   `yaml:"strategy" json:"strategy"`
	MaxSchedules    int      `yaml:"maxSchedules" json:"maxSchedules"`
	MaxPointVisits  int      `yaml:"maxPointVisits,omitempty" json:"maxPointVisits"`
	ScheduleTimeout Duration `yaml:"scheduleTimeout,omitempty" json:"scheduleTimeout"`
	DrainTimeout    Duration `yaml:"drainTimeout,omitempty" json:"drainTimeout"`
	CancelAt        []string `yaml:"cancelAt,omitempty" json:"cancelAt"`
	Seed            int64    `yaml:"seed,omitempty" json:"seed"`
}

type ContractSpec struct {
	Name       string   `yaml:"name" json:"name"`
	Version    int      `yaml:"version,omitempty" json:"version"`
	Severity   string   `yaml:"severity" json:"severity"`
	Requires   []string `yaml:"requires,omitempty" json:"requires"`
	Boundary   string   `yaml:"boundary,omitempty" json:"boundary,omitempty"`
	Expression string   `yaml:"expression" json:"expression"`
}

type OutputSpec struct {
	Directory string `yaml:"directory,omitempty" json:"directory"`
	Format    string `yaml:"format,omitempty" json:"format"`
}
