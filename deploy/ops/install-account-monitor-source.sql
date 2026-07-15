\set ON_ERROR_STOP on
\ir ../../extensions-self/account-monitor/sql/main_source_views.sql

SELECT NULLIF(:'account_monitor_password', '') IS NOT NULL AS account_monitor_password_set \gset
\if :account_monitor_password_set
\else
  \echo 'account_monitor_password must be provided'
  \quit 3
\endif

SELECT format(
    'CREATE ROLE extensions_self_monitor LOGIN PASSWORD %L',
    :'account_monitor_password'
)
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'extensions_self_monitor')
\gexec

ALTER ROLE extensions_self_monitor
    LOGIN
    INHERIT
    NOSUPERUSER
    NOCREATEDB
    NOCREATEROLE
    NOREPLICATION
    NOBYPASSRLS;

SELECT format(
    'ALTER ROLE extensions_self_monitor PASSWORD %L',
    :'account_monitor_password'
)
\gexec

GRANT extensions_self_monitor_ro TO extensions_self_monitor;
