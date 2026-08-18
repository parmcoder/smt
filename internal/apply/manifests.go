package apply

const apiGoMod = `module example.com/smt/apis

go 1.26.5

require (
	github.com/danielgtaylor/huma/v2 v2.39.1
)

require (
	github.com/golangci/golangci-lint/v2 v2.12.2
	golang.org/x/vuln v1.7.0
)

tool (
	github.com/golangci/golangci-lint/v2/cmd/golangci-lint
	golang.org/x/vuln/cmd/govulncheck
)
`

const apiGoSum = `github.com/danielgtaylor/huma/v2 v2.39.1 h1:0kwF4ltQoYZ+IU55VPy+BcGekzgF44R64daTGde1H+g=
github.com/danielgtaylor/huma/v2 v2.39.1/go.mod h1:zcnQ38duIJ3VUHwFaBoZ6x8T+KN/mr33oyqxcj0HTug=
github.com/golangci/golangci-lint/v2 v2.12.2 h1:7+d1uY0bq1MU2UV3R5pW5Q7QWdcoq4naMRXM+gsJKrs=
github.com/golangci/golangci-lint/v2 v2.12.2/go.mod h1:opqHHuIcTG2R+4akzWMd4o1BnD9/1LcjICWOujr91U8=
golang.org/x/vuln v1.7.0 h1:4MQBuhmXbz2uepNJrf3v+aaZLGDqw1JluwYboegA1qg=
golang.org/x/vuln v1.7.0/go.mod h1:Xw7zvU3e1bsCYYBXu+w4wcn2Kgn27f34WBCTw8LL5Us=
`

const apiDatabaseGoMod = `module example.com/smt/apis

go 1.26.5

require (
	github.com/danielgtaylor/huma/v2 v2.39.1
	github.com/golang-migrate/migrate/v4 v4.19.1
	github.com/jackc/pgx/v5 v5.10.0
)

require (
	github.com/golangci/golangci-lint/v2 v2.12.2
	golang.org/x/vuln v1.7.0
)

tool (
	github.com/golang-migrate/migrate/v4/cmd/migrate
	github.com/golangci/golangci-lint/v2/cmd/golangci-lint
	golang.org/x/vuln/cmd/govulncheck
)
`

const apiDatabaseGoSum = `github.com/danielgtaylor/huma/v2 v2.39.1 h1:0kwF4ltQoYZ+IU55VPy+BcGekzgF44R64daTGde1H+g=
github.com/danielgtaylor/huma/v2 v2.39.1/go.mod h1:zcnQ38duIJ3VUHwFaBoZ6x8T+KN/mr33oyqxcj0HTug=
github.com/golang-migrate/migrate/v4 v4.19.1 h1:OCyb44lFuQfYXYLx1SCxPZQGU7mcaZ7gH9yH4jSFbBA=
github.com/golang-migrate/migrate/v4 v4.19.1/go.mod h1:CTcgfjxhaUtsLipnLoQRWCrjYXycRz/g5+RWDuYgPrE=
github.com/golangci/golangci-lint/v2 v2.12.2 h1:7+d1uY0bq1MU2UV3R5pW5Q7QWdcoq4naMRXM+gsJKrs=
github.com/golangci/golangci-lint/v2 v2.12.2/go.mod h1:opqHHuIcTG2R+4akzWMd4o1BnD9/1LcjICWOujr91U8=
github.com/jackc/pgx/v5 v5.10.0 h1:VhSvgU2jSli8o3AqIEOTJr7rZwAEUVo4E4XhR94Zfr0=
github.com/jackc/pgx/v5 v5.10.0/go.mod h1:mal1tBGAFfLHvZzaYh77YS/eC6IX9OWbRV1QIIM0Jn4=
golang.org/x/vuln v1.7.0 h1:4MQBuhmXbz2uepNJrf3v+aaZLGDqw1JluwYboegA1qg=
golang.org/x/vuln v1.7.0/go.mod h1:Xw7zvU3e1bsCYYBXu+w4wcn2Kgn27f34WBCTw8LL5Us=
`

func apiManifests(databaseSelected bool) (string, string) {
	if databaseSelected {
		return apiDatabaseGoMod, apiDatabaseGoSum
	}
	return apiGoMod, apiGoSum
}
