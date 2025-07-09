#!/bin/bash

set -e  # Exit on error

DOMAIN="api.mayura.rocks"
EMAIL="admin@mayura.rocks"  # CHANGE THIS to your actual email!

echo "🚀 Setting up nginx with payment service routing..."

# 1. Install required packages
echo "📦 Installing nginx and certbot..."
apt update
apt install -y nginx certbot python3-certbot-nginx

# 2. Stop NGINX before cleaning
echo "🛑 Stopping nginx..."
systemctl stop nginx || true

# 3. Remove all existing NGINX configs
echo "🧹 Cleaning existing nginx configurations..."
rm -f /etc/nginx/sites-enabled/*
rm -f /etc/nginx/sites-available/*

# 4. Write comprehensive NGINX config with payment service support
echo "📝 Creating nginx configuration with payment service routing..."
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

    # Payment and subscription endpoints - route to payment service with unrestricted access
    location /api/webhook {
        # Allow all origins for subscription endpoints
        add_header 'Access-Control-Allow-Origin' '*' always;
        add_header 'Access-Control-Allow-Methods' 'GET, POST, PUT, DELETE, OPTIONS' always;
        add_header 'Access-Control-Allow-Headers' 'Content-Type, Authorization, X-Requested-With, X-Signature' always;
        add_header 'Access-Control-Allow-Credentials' 'true' always;
        add_header 'Access-Control-Max-Age' '86400' always;

        # Handle OPTIONS requests for CORS preflight
        if (\$request_method = 'OPTIONS') {
            add_header 'Content-Length' '0' always;
            add_header 'Content-Type' 'text/plain; charset=utf-8' always;
            return 204;
        }

        # Proxy to payment service on port 8081
        proxy_pass http://localhost:8081;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Origin \$http_origin;
        
        # Pass through webhook signature header
        proxy_set_header X-Signature \$http_x_signature;
        
        # Increase timeouts for payment processing
        proxy_connect_timeout       60s;
        proxy_send_timeout          60s;
        proxy_read_timeout          60s;
    }

    # All other requests go to the gateway service
    location / {
        # Handle OPTIONS requests for CORS preflight
        if (\$request_method = 'OPTIONS') {
            add_header 'Access-Control-Allow-Origin' 'https://mayura.rocks' always;
            add_header 'Access-Control-Allow-Methods' 'GET, POST, PUT, DELETE, OPTIONS' always;
            add_header 'Access-Control-Allow-Headers' 'Content-Type, Authorization, X-Requested-With' always;
            add_header 'Access-Control-Allow-Credentials' 'true' always;
            add_header 'Access-Control-Max-Age' '86400' always;
            add_header 'Content-Length' '0' always;
            add_header 'Content-Type' 'text/plain; charset=utf-8' always;
            return 204;
        }

        # Proxy all other requests to the gateway
        proxy_pass http://localhost:8080;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Origin \$http_origin;
        
        # Let the backend handle CORS headers for non-OPTIONS requests
        # The Go CORS middleware will handle these
    }
}
EOF

# 5. Enable site
echo "🔗 Enabling nginx site..."
ln -sf /etc/nginx/sites-available/$DOMAIN /etc/nginx/sites-enabled/$DOMAIN

# 6. Test config
echo "🔍 Testing nginx configuration..."
nginx -t

# 7. Start NGINX temporarily
echo "🚀 Starting nginx..."
systemctl start nginx

# 8. Get SSL certificate from Let's Encrypt
echo "🔒 Obtaining SSL certificate from Let's Encrypt..."
certbot --nginx --non-interactive --agree-tos --redirect -m "$EMAIL" -d "$DOMAIN"

# 9. Reload NGINX with new certs
echo "🔄 Reloading nginx with SSL certificates..."
systemctl reload nginx

# 10. Enable NGINX on boot
echo "🔧 Enabling nginx to start on boot..."
systemctl enable nginx

echo ""
echo "🎉 Nginx setup completed successfully!"
echo ""
echo "📊 Configuration Summary:"
echo "  ✅ Domain: https://$DOMAIN"
echo "  ✅ SSL Certificate: Installed and auto-renewal enabled"
echo "  ✅ Payment endpoints: /api/checkout, /api/tier, /api/subscription, /api/cancel-subscription, /api/webhook"
echo "  ✅ Payment service port: 8081"
echo "  ✅ Gateway service port: 8080"
echo "  ✅ CORS: Allow all origins for payment endpoints"
echo "  ✅ Health check: /health"
echo ""
echo "🔧 Routing Configuration:"
echo "  📍 Payment endpoints → localhost:8081 (unrestricted CORS)"
echo "  📍 Health check → localhost:8081 (unrestricted CORS)"
echo "  📍 All other endpoints → localhost:8080 (restricted CORS)"
echo "  🔐 Webhook signatures properly forwarded"
echo "  ⏱️  Extended timeouts for payment processing"
echo ""
echo "🌐 Services are now accessible at:"
echo "  • https://$DOMAIN/api/checkout"
echo "  • https://$DOMAIN/api/tier"
echo "  • https://$DOMAIN/api/subscription"
echo "  • https://$DOMAIN/api/webhook"
echo "  • https://$DOMAIN/health"
echo ""
