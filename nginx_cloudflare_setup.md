# Nginx & Cloudflare Subdomain Setup Guide

This guide details how to expose your containerized Go application to the internet under a custom subdomain (e.g., `whatsapp.yourdomain.com`) using **Cloudflare** for DNS/SSL and **Nginx** on your VPS as a reverse proxy.

---

## ☁️ Step 1: Cloudflare Subdomain Configuration

To point your subdomain to your VPS:

1. **Log in** to your Cloudflare dashboard and select your domain.
2. Go to **DNS > Records** and click **Add Record**.
3. Configure the record details:
   - **Type**: `A`
   - **Name**: Your subdomain (e.g., `whatsapp` for `whatsapp.yourdomain.com`, or `@` if using the root domain).
   - **IPv4 address**: Your VPS public IP address (e.g., `43.133.158.142`).
   - **Proxy status**: **Proxied** (Orange Cloud) — *This hides your VPS IP address, protects against DDoS, and enables Cloudflare SSL.*
   - **TTL**: `Auto`
4. Click **Save**.

### Configure Cloudflare SSL/TLS Mode
1. In Cloudflare, navigate to **SSL/TLS > Overview**.
2. Select **Full (Strict)**. 
   > [!IMPORTANT]
   > **Full (Strict)** mode secures the connection end-to-end: encrypting traffic from the user to Cloudflare, and from Cloudflare to your Nginx server using a valid/trusted certificate (like Let's Encrypt) or a Cloudflare Origin CA certificate.

---

## 🖥️ Step 2: Install and Configure Nginx on VPS

SSH into your VPS to set up Nginx.

### 1. Install Nginx
On Debian/Ubuntu systems:
```bash
sudo apt update
sudo apt install nginx -y
```

On CentOS/Rocky Linux/RHEL:
```bash
sudo dnf install epel-release -y
sudo dnf install nginx -y
sudo systemctl enable nginx --now
```

### 2. Configure the Nginx Server Block
Create a new Nginx configuration file. We will configure Nginx to listen on port `80` (HTTP) and proxy the traffic to your container's host port (e.g., `13000` or whatever you set in `PORT` / docker-compose):

```bash
sudo nano /etc/nginx/sites-available/kiw-test
```

Paste the following configuration (replace `whatsapp.yourdomain.com` with your actual subdomain, and adjust `13000` if your host port is different):

```nginx
# Redirect HTTP to HTTPS
server {
    listen 80;
    listen [::]:80;
    server_name whatsapp.yourdomain.com;

    return 301 https://$host$request_uri;
}

# HTTPS Server Block
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name whatsapp.yourdomain.com;

    # SSL Certificates (We will generate these in Step 3)
    ssl_certificate /etc/letsencrypt/live/whatsapp.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/whatsapp.yourdomain.com/privkey.pem;

    # Safe SSL protocols and ciphers (Cloudflare recommended)
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers 'ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384';
    ssl_prefer_server_ciphers on;

    # Logs
    access_log /var/log/nginx/kiw_whatsapp_access.log;
    error_log /var/log/nginx/kiw_whatsapp_error.log;

    location / {
        # Forward traffic to the container's host port
        proxy_pass http://127.0.0.1:13000;
        
        # Standard proxy headers
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Webhook timeout settings
        proxy_connect_timeout 90;
        proxy_send_timeout 90;
        proxy_read_timeout 90;
    }
}
```

### 3. Enable the Configuration
Link the configuration to `sites-enabled` and test the Nginx syntax:

```bash
# Enable configuration
sudo ln -s /etc/nginx/sites-available/kiw-test /etc/nginx/sites-enabled/

# Test Nginx syntax
sudo nginx -t
```
If the test is successful, restart Nginx:
```bash
sudo systemctl restart nginx
```

---

## 🔒 Step 3: Obtain SSL Certificates (Let's Encrypt / Certbot)

Since you configured **Full (Strict)** mode on Cloudflare, Nginx needs a valid certificate. The easiest way is using Let's Encrypt via Certbot.

### 1. Install Certbot
On Debian/Ubuntu:
```bash
sudo apt install certbot python3-certbot-nginx -y
```

### 2. Obtain and Install Certificate
Run Certbot to generate certificates. Certbot will read your Nginx configurations and automatically request and bind the certificates:

```bash
sudo certbot --nginx -d whatsapp.yourdomain.com
```

- Follow the prompt (enter email, accept terms).
- Certbot will obtain the certificate and update your Nginx files automatically with the correct `ssl_certificate` and `ssl_certificate_key` paths.

### 3. Verify Auto-Renewal
Let's Encrypt certificates are valid for 90 days. Certbot configures an automatic renewal task. Test it with:
```bash
sudo certbot renew --dry-run
```

---

## 🛡️ Alternative: Using Cloudflare Origin CA Certificates

Instead of Let's Encrypt, you can generate an SSL certificate directly inside Cloudflare that is valid for up to 15 years:

1. In Cloudflare, go to **SSL/TLS > Origin Server** and click **Create Certificate**.
2. Keep the defaults (valid for 15 years, covers your subdomain) and click **Create**.
3. Copy the **Origin Certificate** and save it on your VPS as `/etc/ssl/certs/cloudflare_origin.pem`.
4. Copy the **Private Key** and save it on your VPS as `/etc/ssl/private/cloudflare_origin.key`.
5. Update your `/etc/nginx/sites-available/kiw-test` configuration to point to these files:
   ```nginx
   ssl_certificate /etc/ssl/certs/cloudflare_origin.pem;
   ssl_certificate_key /etc/ssl/private/cloudflare_origin.key;
   ```
6. Restart Nginx: `sudo systemctl restart nginx`.
