if (.check_runs | type) != "array" then
  error("check_runs must be an array")
else
  .check_runs as $runs
  | [$expected[] as $name
    | ([$runs[] | select(.name == $name)] | last) as $run
    | if $run == null then
        {name: $name, status: "missing", conclusion: "missing", html_url: ""}
      else
        {
          name: $name,
          status: ($run.status // ""),
          conclusion: ($run.conclusion // ""),
          html_url: ($run.html_url // "")
        }
      end] as $summary
  | ($summary | map(select(.status == "completed" and .conclusion != "success")) | first) as $failed
  | ($summary | map(select(.status == "missing")) | first) as $missing
  | ($summary | map(select(.status != "completed")) | first) as $pending
  | if $failed != null then
      {
        ok: false,
        message: "required check \($failed.name) concluded \($failed.conclusion)",
        error_code: "ACTIONS_REQUIRED_CHECK_FAILED",
        failed_check: $failed.name,
        check_url: $failed.html_url,
        conclusion: $failed.conclusion,
        workflow_url: $failed.html_url,
        production_changed: false
      }
    elif $missing != null then
      {
        ok: null,
        message: "required check \($missing.name) is missing",
        error_code: "ACTIONS_REQUIRED_CHECK_MISSING",
        failed_check: $missing.name,
        check_url: "",
        conclusion: "missing",
        workflow_url: "",
        production_changed: false
      }
    elif $pending != null then
      {
        ok: null,
        message: "required check \($pending.name) is \($pending.status)",
        error_code: "ACTIONS_REQUIRED_CHECK_PENDING",
        failed_check: $pending.name,
        check_url: $pending.html_url,
        conclusion: $pending.status,
        workflow_url: $pending.html_url,
        production_changed: false
      }
    else
      ($summary | map(select(.name == "images")) | first) as $images
      | {
          ok: true,
          message: "all required GitHub Actions checks succeeded",
          error_code: "",
          failed_check: "",
          check_url: "",
          conclusion: "success",
          workflow_url: $images.html_url,
          production_changed: false
        }
    end
end
