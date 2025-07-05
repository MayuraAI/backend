#!/bin/bash

DOMAIN="api.mayura.rocks"

# 1. Install required packages
apt update
apt install -y nginx certbot python3-certbot-nginx

# 2. Stop NGINX before cleaning
systemctl stop nginx

# 3. Remove all existing NGINX configs
rm -f /etc/nginx/sites-enabled/*
rm -f /etc/nginx/sites-available/*

# 4. Write new config
cat > /etc/nginx/sites-available/$DOMAIN <<EOF
$(cat <<'CONFIG'
<PASTE THE NGINX CONFIG HERE FROM STEP 1>
CONFIG
)
EOF

# Replace placeholder with actual config
sed -i "s|<PASTE THE NGINX CONFIG HERE FROM STEP 1>|$(cat <<'EOC'
server {
    listen 80;
    server_name api.mayura.rocks;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name api.mayura.rocks;

    ssl_certificate /etc/letsencrypt/live/api.mayura.rocks/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/api.mayura.rocks/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;

    add_header X-Frame-Options DENY;
    add_header X-Content-Type-Options nosniff;
    add_header Referrer-Policy no-referrer-when-downgrade;
    add_header Strict-Transport-Security "max-age=63072000; includeSubDomains; preload" always;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
EOC
)" /etc/nginx/sites-available/$DOMAIN

# 5. Enable site
ln -s /etc/nginx/sites-available/$DOMAIN /etc/nginx/sites-enabled/

# 6. Start NGINX temporarily
systemctl start nginx

# 7. Get SSL certificate
certbot --nginx --non-interactive --agree-tos --redirect -m you@example.com -d $DOMAIN

# 8. Reload NGINX with HTTPS certs
systemctl reload nginx

# 9. Enable on boot
systemctl enable nginx

echo "✅ HTTPS NGINX proxy is ready for https://$DOMAIN → localhost:8080"
