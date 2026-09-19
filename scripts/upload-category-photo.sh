#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
if [ "$#" -ne 3 ]; then
  echo "usage: $0 DEPARTMENT CATEGORY IMAGE" >&2
  exit 2
fi
department=$1
category=$2
image=$3
if [ ! -f "$image" ]; then echo "image not found: $image" >&2; exit 2; fi

if [ -z "${CLOUDINARY_CLOUD_NAME:-}" ]; then
  set -a
  source .env
  set +a
fi
: "${CLOUDINARY_CLOUD_NAME:?Cloudinary cloud name is missing}"
: "${CLOUDINARY_API_KEY:?Cloudinary API key is missing}"
: "${CLOUDINARY_API_SECRET:?Cloudinary API secret is missing}"

slug() { printf '%s' "$1" | sed -E 's/[^A-Za-z0-9]+/_/g; s/^_+//; s/_+$//'; }
folder="Lamazon/Categories/$(slug "$department")"
name=$(slug "$category")
public_id="$folder/$name"
timestamp=$(date +%s)
signed="asset_folder=$folder&display_name=$name&invalidate=true&overwrite=true&public_id=$public_id&timestamp=$timestamp$CLOUDINARY_API_SECRET"
signature=$(printf '%s' "$signed" | openssl dgst -sha1 -r | awk '{print $1}')
response=$(curl -fsS "https://api.cloudinary.com/v1_1/$CLOUDINARY_CLOUD_NAME/auto/upload" \
  -F "api_key=$CLOUDINARY_API_KEY" -F "timestamp=$timestamp" \
  -F "public_id=$public_id" -F "asset_folder=$folder" \
  -F "display_name=$name" -F "overwrite=true" -F "invalidate=true" \
  -F "signature=$signature" -F "file=@$image")
printf '%s' "$response" | jq -er '.secure_url'
