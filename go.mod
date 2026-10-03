module herdr-sandbox

go 1.26.7

require (
	github.com/BurntSushi/toml v1.6.0
	rsc.io/qr v0.2.0
)

require (
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/mod v0.39.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/telemetry v0.0.0-20260811182544-a038080d80e5 // indirect
	golang.org/x/tools v0.49.0 // indirect
	golang.org/x/vuln v1.7.0 // indirect
	honnef.co/go/tools v0.7.0 // indirect
)

tool (
	golang.org/x/tools/go/analysis/passes/nilness/cmd/nilness
	golang.org/x/vuln/cmd/govulncheck
	honnef.co/go/tools/cmd/staticcheck
)
