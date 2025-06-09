# PullPilot

PullPilot is your AI assistant for pull requests — built to streamline PR reviews by generating smart summaries and enabling maintainers to chat directly with the contents of the changes. It’s like GitHub Copilot, but for PRs.

## What It Does

PullPilot enhances the pull request workflow by:

-  **Auto-generating a summary comment** when a new pull request is opened, explaining what changed and why.
-  **Launching a chat interface for maintainers** to talk directly with the code and files involved in the PR.
-  **Understanding application-level context**, so the feedback and conversations are grounded in how the code fits into the larger project.

---

## Features

- **PR Comment Bot**: Adds an intelligent comment with a high-level summary of changes, potential risks, and areas of focus for review.
- **Code-Aware Chat Interface**: Lets maintainers ask questions like _“What does this change affect in the app logic?”_ or _“Show me where this modifies the authentication flow.”_
- **Contextual Awareness**: Chat has access to:
  - Entire changed files
  - Project structure
  - Relevant parts of the application context (e.g. architecture, core files)

## GitHub Actions Integration
```yaml
name: Code Review

on:
  pull_request:
    branches: [main, master]

jobs:
  review:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v3
        
      - name: Run AI Code Review
        uses: Per0x1de-1337/PullPilot@latest
        with:
          pr_url: ${{ github.event.pull_request.html_url }}
          command_for_running_application: 'docker compose up'
          github_token: ${{ secrets.PAT_OF_GITHUB }}
          repo: ${{ github.repository }}
          gemini_api_key: ${{ secrets.GEMINI_API_KEY }}
          location_from_where_we_have_to_execute_the_command: 'samples'
          ports: '5000 1639'
          bot_app_id: ${{ secrets.BOT_APP_ID }}
          bot_installation_id: ${{ secrets.BOT_INSTALLATION_ID }}
          bot_private_key: ${{ secrets.BOT_PRIVATE_KEY }}
          delay_in_seconds: 60
          path_of_script_sh: '.'
```
### Required Secrets
Make sure to set the following secrets in your GitHub repository:
`PAT_OF_GITHUB`
`GEMINI_API_KEY`
`BOT_APP_ID`
`BOT_INSTALLATION_ID`
`BOT_PRIVATE_KEY`

## Demo
https://drive.google.com/file/d/1dHXZpJRW5OnqM7GOEV1sLpTT0k9CoAZb/view
