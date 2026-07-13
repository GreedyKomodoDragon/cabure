#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: hack/create-git-ssh-secret.sh -n <secret-name> -N <namespace> -o <output-dir> -k <known_hosts_file> [-f]

Generates a new Ed25519 SSH keypair and a Kubernetes Secret manifest of type
kubernetes.io/ssh-auth containing the private key and known_hosts.

Outputs:
  - <output-dir>/<secret-name>.pub
  - <output-dir>/<secret-name>
  - <output-dir>/<secret-name>.secret.yaml

The manifest is not applied automatically. Use kubectl apply -f if desired.
EOF
}

secret_name=""
namespace=""
output_dir=""
force=0
known_hosts_file=""

while getopts ":n:N:o:k:f" opt; do
  case "$opt" in
    n) secret_name="$OPTARG" ;;
    N) namespace="$OPTARG" ;;
    o) output_dir="$OPTARG" ;;
    k) known_hosts_file="$OPTARG" ;;
    f) force=1 ;;
    *) usage >&2; exit 1 ;;
  esac
done

if [[ -z "$secret_name" || -z "$namespace" || -z "$output_dir" || -z "$known_hosts_file" ]]; then
  usage >&2
  exit 1
fi

mkdir -p "$output_dir"

private_key="$output_dir/$secret_name"
public_key="$output_dir/$secret_name.pub"
secret_yaml="$output_dir/$secret_name.secret.yaml"

if [[ -e "$private_key" || -e "$public_key" || -e "$secret_yaml" ]]; then
  if [[ "$force" -ne 1 ]]; then
    echo "Refusing to overwrite existing files. Re-run with -f to replace." >&2
    exit 1
  fi
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

ssh-keygen -t ed25519 -f "$tmpdir/id_ed25519" -N "" -C "$secret_name@$namespace" >/dev/null
cp "$tmpdir/id_ed25519" "$private_key"
cp "$tmpdir/id_ed25519.pub" "$public_key"

cat >"$secret_yaml" <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: $secret_name
  namespace: $namespace
type: kubernetes.io/ssh-auth
stringData:
  ssh-privatekey: |
$(sed 's/^/    /' "$private_key")
  known_hosts: |
$(sed 's/^/    /' "$known_hosts_file")
EOF

chmod 600 "$private_key"
chmod 644 "$public_key" "$secret_yaml"

echo "Wrote:"
echo "  $private_key"
echo "  $public_key"
echo "  $secret_yaml"
echo
echo "Public key:"
cat "$public_key"
echo
echo "Known hosts source:"
cat "$known_hosts_file"
