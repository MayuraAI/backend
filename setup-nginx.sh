#!/bin/bash

# Production Nginx Setup Script for api.mayura.rocks
# This script configures nginx with Let's Encrypt SSL certificates

set -e

echo "Setting up nginx for production at api.mayura.rocks..."

# Check if domain resolves to this server
echo "Checking DNS resolution..."
CURRENT_IP=$(curl -s ifconfig.me)
DOMAIN_IP=$(dig +short api.mayura.rocks)

if [ "$CURRENT_IP" != "$DOMAIN_IP" ]; then
    echo "WARNING: Domain api.mayura.rocks does not resolve to this server IP ($CURRENT_IP)"
    echo "Domain resolves to: $DOMAIN_IP"
    echo "Please update your DNS records and wait for propagation before continuing."
    read -p "Continue anyway? (y/N): " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# Update system packages
echo "Updating system packages..."
sudo apt update

# Install nginx if not already installed
if ! command -v nginx &> /dev/null; then
    echo "Installing nginx..."
    sudo apt install -y nginx
fi

# Install certbot for Let's Encrypt
echo "Installing certbot..."
sudo apt install -y certbot python3-certbot-nginx

# Copy rate limiting configuration
echo "Installing rate limiting configuration..."
sudo cp nginx-rate-limit.conf /etc/nginx/conf.d/

# Create temporary nginx config without SSL for initial setup
echo "Creating temporary nginx configuration..."
sudo tee /etc/nginx/sites-available/api.mayura.rocks > /dev/null <<EOF
server {
    listen 80;
    server_name api.mayura.rocks;
    
    # Proxy Configuration for initial setup
    location / {
        proxy_pass http://localhost:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_cache_bypass \$http_upgrade;
    }
    
    # Health Check Endpoint
    location /health {
        access_log off;
        proxy_pass http://localhost:8080/health;
    }
    
    # Hide nginx version
    server_tokens off;
}
EOF

# Enable the site
sudo ln -sf /etc/nginx/sites-available/api.mayura.rocks /etc/nginx/sites-enabled/

# Remove default nginx site if it exists
sudo rm -f /etc/nginx/sites-enabled/default

# Test nginx configuration
echo "Testing nginx configuration..."
sudo nginx -t

# Restart nginx
echo "Restarting nginx..."
sudo systemctl restart nginx

# Enable nginx to start on boot
sudo systemctl enable nginx

# Wait for backend services to be ready
# echo "Waiting for backend services to be ready..."
# sleep 10

# Test if backend is responding
# if curl -f http://localhost:8080/health > /dev/null 2>&1; then
#     echo "Backend is responding on port 8080"
# else
#     echo "WARNING: Backend is not responding on port 8080"
#     echo "Make sure your docker-compose services are running:"
#     echo "docker-compose up -d"
#     read -p "Continue with SSL setup anyway? (y/N): " -n 1 -r
#     echo
#     if [[ ! $REPLY =~ ^[Yy]$ ]]; then
#         exit 1
#     fi
# fi

# Get SSL certificate from Let's Encrypt
echo "Obtaining SSL certificate from Let's Encrypt..."
sudo certbot --nginx -d api.mayura.rocks --non-interactive --agree-tos --email pavanmanishd@gmail.com --redirect

# Replace with our secure configuration
echo "Installing secure nginx configuration..."
sudo tee /etc/nginx/sites-available/api.mayura.rocks > /dev/null <<EOF
server {
    listen 80;
    server_name api.mayura.rocks;
    
    # Redirect HTTP to HTTPS
    return 301 https://\$server_name\$request_uri;
}

server {
    listen 443 ssl http2;
    server_name api.mayura.rocks;
    
    # SSL Configuration (managed by certbot)
    ssl_certificate /etc/letsencrypt/live/api.mayura.rocks/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/api.mayura.rocks/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;
    
    # Additional SSL Security Settings (some may be in options-ssl-nginx.conf)
    ssl_session_cache shared:SSL:10m;
    ssl_session_tickets off;
    
    # Security Headers
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
    add_header X-Frame-Options DENY always;
    add_header X-Content-Type-Options nosniff always;
    add_header X-XSS-Protection "1; mode=block" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;
    
    # Basic DDoS Protection
    client_max_body_size 10M;
    client_body_timeout 60s;
    client_header_timeout 60s;
    
    # Rate Limiting (zone defined in main nginx.conf)
    limit_req zone=api burst=20 nodelay;
    
    # Proxy Configuration
    location / {
        proxy_pass http://localhost:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_cache_bypass \$http_upgrade;
        
        # Timeouts
        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
    }
    
    # Health Check Endpoint
    location /health {
        access_log off;
        proxy_pass http://localhost:8080/health;
    }
    
    # Hide nginx version
    server_tokens off;
}
EOF

# Test final nginx configuration
echo "Testing final nginx configuration..."
sudo nginx -t

# Restart nginx with new configuration
echo "Restarting nginx with SSL configuration..."
sudo systemctl restart nginx

# Set up automatic certificate renewal
echo "Setting up automatic certificate renewal..."
sudo crontab -l > /tmp/crontab_backup || true
echo "0 12 * * * /usr/bin/certbot renew --quiet && systemctl reload nginx" | sudo tee -a /tmp/crontab_backup
sudo crontab /tmp/crontab_backup
rm /tmp/crontab_backup

echo ""
echo "🎉 Production setup complete!"
echo "Your API is now available at https://api.mayura.rocks"
echo ""
echo "SSL Certificate Details:"
sudo certbot certificates -d api.mayura.rocks
echo ""
echo "✅ HTTPS is properly configured"
echo "✅ HTTP automatically redirects to HTTPS"
echo "✅ SSL certificates will auto-renew"
echo "✅ Security headers are active"
echo "✅ Rate limiting is enabled"
echo ""
echo "Test your API:"
echo "curl https://api.mayura.rocks/health" 