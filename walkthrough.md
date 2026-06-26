# Deployment Setup Walkthrough

We have successfully containerized your Go WhatsApp application and set up a GitLab CI/CD configuration to deploy to your VPS using Podman.

---

## 🛠️ Changes Implemented

### 1. Created [Dockerfile](file:///home/endru/Code/kiw-test/Dockerfile)
- A multi-stage Docker build config.
- Copies both `go.mod` and `go.sum` to enable dependency caching while ensuring module checksums match during download.
- Copies `dashboard.html` into the runtime image to serve the frontend dashboard.
- Pulls from fully qualified registries (`docker.io/library/golang:1.26-alpine` and `docker.io/library/alpine:3.21`) to prevent search registry warnings on Podman.
- Installs CA certificates in the runtime image to allow SSL requests (essential for calling WhatsApp's Graph API).
- Uses Go compiler optimization flags (`-ldflags="-s -w"`) to reduce binary size.

### 2. Created [docker-compose.yml](file:///home/endru/Code/kiw-test/docker-compose.yml)
- Configured the deployment service to read environment variables from a local `.env` file on the VPS.
- Binds container port 8080 to your configured host port (defaults to 8080).
- Standard security options added (`no-new-privileges:true`) for secure container execution.

### 3. Created [.gitlab-ci.yml](file:///home/endru/Code/kiw-test/.gitlab-ci.yml)
- **build_image stage**: Compiles and packages the application container, then uploads it to the GitLab Container Registry.
- **deploy stage**: Connects via SSH to your VPS, writes/updates the local `.env` configuration file dynamically with GitLab variables, updates `docker-compose.yml`, and commands Podman (via `podman-compose`, `docker-compose`, or direct `podman run`) to pull the new container and restart it.

---

## 🧪 Verification Results

We verified the `Dockerfile` locally using `podman build`:
```bash
podman build -t kiw-test-local .
```
The build completed successfully:
```
[1/2] STEP 7/7: RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o server ./cmd/server/main.go
--> a720628bb15b
[2/2] STEP 2/6: RUN apk --no-cache add ca-certificates
OK: 7 MiB in 16 packages
--> 3b8b42d62d23
[2/2] STEP 4/6: COPY --from=builder /app/server .
--> 0dcb405bef05
[2/2] COMMIT kiw-test-local
Successfully tagged localhost/kiw-test-local:latest
68ba7b5d6a61092d91f218bd6b565b6d3abea2414e557fe11a028383ff99554e
```

---

## 🚀 Next Steps to Complete Deployment

1. **Set Up your SSH Key on the VPS & GitLab:**
   - Follow the SSH key configuration instructions in the [Implementation Plan](file:///home/endru/.gemini/antigravity-cli/brain/33c2dd9a-ab6b-4416-b043-eb2235c8db40/implementation_plan.md#step-1-create-an-ssh-key-for-gitlab) to add the public key to your VPS `~/.ssh/authorized_keys` and save the private key in GitLab CI/CD Variables.

2. **Add Environment Variables to GitLab:**
   - In GitLab under **Settings > CI/CD > Variables**, add your keys/secrets (`WHATSAPP_ACCESS_TOKEN`, `WHATSAPP_PHONE_NUMBER_ID`, `WHATSAPP_VERIFY_TOKEN`, `VPS_IP`, `VPS_USER`, and `SSH_PRIVATE_KEY`).

3. **Push to GitLab:**
   - Configure your Git remote and push the `main` branch to GitLab. The pipeline will trigger automatically, build the container, and deploy it to your VPS.
