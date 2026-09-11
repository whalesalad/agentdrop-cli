package cli

const helpText = `AgentDrop — files and readable responses in the cloud

  agentdrop [open] FILE        Upload and open a one-hour reader link
  cat answer.md | agentdrop   Upload Markdown from stdin
  agentdrop put FILE          Upload privately; print the file ID
  agentdrop get ID -o FILE    Download original bytes
  agentdrop list              List your recent files
  agentdrop info ID           Get file metadata
  agentdrop share ID          Create a reader link
  agentdrop delete ID --yes   Delete a file and invalidate its links
  agentdrop revoke ID --yes   Revoke a share by its management ID
  agentdrop login             Approve a named API token in your browser
  agentdrop login --no-open   Display a URL/code for approval on another device
  agentdrop exec -- COMMAND   Launch a client with its API token environment
  agentdrop logout            Remove this profile’s locally saved API token
  agentdrop whoami            Check personal vault access

Options:
  -h, --help                 Show help
  -o, --output FILE          Save downloaded bytes (replaces a regular file)
  -n, --name NAME            Upload filename or login client name; stdin: response.md
      --type TYPE            Upload media type
      --expires DURATION     5m, 30m, 1h (default), 1d, 7d
      --no-open              Return the reader URL without opening a browser
      --json                 Return structured metadata
  -y, --yes                  Confirm delete/revoke
      --limit N              List up to 100 files (default 20)
      --cursor CURSOR        Continue a listing
      --profile NAME         Separate client credential profile (default: default)
      --version              Print the CLI version

Authentication: AGENTDROP_API_TOKEN or agentdrop login. Vault keys stay in the browser.
API: AGENTDROP_API_URL (default https://agentdrop.lol).
open uploads your content and creates a temporary link accessible to anyone
holding it. Files remain in your personal vault after links expire. put stays private.
Clipboard tools compose normally: pbpaste | agentdrop.
`
