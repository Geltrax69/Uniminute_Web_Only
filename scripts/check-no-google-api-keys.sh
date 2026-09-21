#!/bin/sh
set -eu

# Google API keys are normally "AIza" followed by 35 URL-safe characters.
# Print filenames only so a CI failure never copies the credential into logs.
if git grep -l -I -E 'AIza[0-9A-Za-z_-]{35}' -- .; then
  echo >&2
  echo "Google API key-shaped value found in tracked source." >&2
  echo "Move the value to runtime configuration before committing." >&2
  exit 1
fi

echo "No Google API key-shaped values found in tracked source."
