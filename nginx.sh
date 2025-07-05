#!/bin/bash

set -e  # Exit on error

DOMAIN="api.mayura.rocks"
EMAIL="admin@mayura.rocks"  # CHANGE THIS to your actual email!

# 1. Install required packages
apt update
apt install -y nginx certbot python3-certbot-nginx

# 2. Stop NGINX before cleaning
systemctl stop nginx || true

# 3. Remove all existing NGINX configs
rm -f /etc/nginx/sites-enabled/*
rm -f /etc/nginx/sites-available/*

# 4. Write new NGINX config
cat <<EOF > /etc/nginx/sites-available/$DOMAIN
server {
    listen 80;
    server_name $DOMAIN;

    # Redirect all HTTP to HTTPS
    return 301 https://\$host\$request_uri;
}

server {
    listen 443 ssl http2;
    server_name $DOMAIN;

    ssl_certificate /etc/letsencrypt/live/$DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$DOMAIN/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;

    add_header X-Frame-Options DENY;
    add_header X-Content-Type-Options nosniff;
    add_header Referrer-Policy no-referrer-when-downgrade;
    add_header Strict-Transport-Security "max-age=63072000; includeSubDomains; preload" always;

    # CORS headers
    add_header Access-Control-Allow-Origin "https://mayura.rocks" always;
    add_header Access-Control-Allow-Methods "GET, POST, PUT, DELETE, OPTIONS" always;
    add_header Access-Control-Allow-Headers "Content-Type, Authorization, X-Requested-With" always;
    add_header Access-Control-Allow-Credentials "true" always;
    add_header Access-Control-Max-Age "86400" always;

    # Handle preflight requests
    if (\$request_method = 'OPTIONS') {
        add_header Access-Control-Allow-Origin "https://mayura.rocks" always;
        add_header Access-Control-Allow-Methods "GET, POST, PUT, DELETE, OPTIONS" always;
        add_header Access-Control-Allow-Headers "Content-Type, Authorization, X-Requested-With" always;
        add_header Access-Control-Allow-Credentials "true" always;
        add_header Access-Control-Max-Age "86400" always;
        add_header Content-Length 0;
        add_header Content-Type text/plain;
        return 204;
    }

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        
        # Additional proxy settings for CORS
        proxy_set_header Origin \$http_origin;
        proxy_hide_header Access-Control-Allow-Origin;
        proxy_hide_header Access-Control-Allow-Methods;
        proxy_hide_header Access-Control-Allow-Headers;
        proxy_hide_header Access-Control-Allow-Credentials;
        proxy_hide_header Access-Control-Max-Age;
    }
}
EOF

# 5. Enable site
ln -sf /etc/nginx/sites-available/$DOMAIN /etc/nginx/sites-enabled/$DOMAIN

# 6. Test config
nginx -t

# 7. Start NGINX temporarily
systemctl start nginx

# 8. Get SSL certificate from Let's Encrypt
certbot --nginx --non-interactive --agree-tos --redirect -m "$EMAIL" -d "$DOMAIN"

# 9. Reload NGINX with new certs
systemctl reload nginx

# 10. Enable NGINX on boot
systemctl enable nginx

echo "✅ HTTPS proxy is live at https://$DOMAIN forwarding to localhost:8080"
