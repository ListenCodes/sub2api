module github.com/Wei-Shaw/sub2api/risk-control

go 1.26.5

require (
	github.com/DATA-DOG/go-sqlmock v1.5.2
	github.com/ListenCodes/sub2api-account-monitor v0.0.0
	github.com/lib/pq v1.10.9
)

replace github.com/ListenCodes/sub2api-account-monitor => ../account-monitor
