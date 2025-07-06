#!/bin/bash

set -e  # Exit on error

DOMAIN="localhost"
NGINX_PORT="80"

echo "🚀 Setting up nginx for development with unrestricted CORS..."

# 1. Install nginx if not already installed
echo "📦 Installing nginx..."
if command -v nginx &> /dev/null; then
    echo "✅ nginx is already installed"
else
    if [[ "$OSTYPE" == "linux-gnu"* ]]; then
        sudo apt update
        sudo apt install -y nginx
    elif [[ "$OSTYPE" == "darwin"* ]]; then
        brew install nginx
    else
        echo "❌ Please install nginx manually for your OS"
        exit 1
    fi
fi

# 2. Stop nginx if running
echo "🛑 Stopping nginx..."
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    sudo systemctl stop nginx || true
elif [[ "$OSTYPE" == "darwin"* ]]; then
    sudo nginx -s stop 2>/dev/null || true
fi

# 3. Create nginx config directory if it doesn't exist
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    CONFIG_DIR="/etc/nginx"
    SITES_DIR="$CONFIG_DIR/sites-available"
    ENABLED_DIR="$CONFIG_DIR/sites-enabled"
    sudo mkdir -p $SITES_DIR $ENABLED_DIR
elif [[ "$OSTYPE" == "darwin"* ]]; then
    CONFIG_DIR="/opt/homebrew/etc/nginx"
    SITES_DIR="$CONFIG_DIR/servers"
    sudo mkdir -p $SITES_DIR
fi

# 4. Remove existing development config
echo "🧹 Cleaning existing development nginx configuration..."
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    sudo rm -f $ENABLED_DIR/dev-backend*
    sudo rm -f $SITES_DIR/dev-backend*
elif [[ "$OSTYPE" == "darwin"* ]]; then
    sudo rm -f $SITES_DIR/dev-backend*
fi

# 5. Create comprehensive development nginx config
echo "📝 Creating development nginx configuration..."
CONFIG_FILE="$SITES_DIR/dev-backend"

if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    sudo tee $CONFIG_FILE > /dev/null <<EOF
server {
    listen 80;
    server_name localhost;

    # Enable CORS for all origins (development only)
    add_header 'Access-Control-Allow-Origin' '*' always;
    add_header 'Access-Control-Allow-Methods' 'GET, POST, PUT, DELETE, OPTIONS, PATCH' always;
    add_header 'Access-Control-Allow-Headers' 'Content-Type, Authorization, X-Requested-With, X-Signature, Accept, Origin' always;
    add_header 'Access-Control-Allow-Credentials' 'true' always;
    add_header 'Access-Control-Max-Age' '86400' always;

    # Handle OPTIONS requests for CORS preflight
    if (\$request_method = 'OPTIONS') {
        add_header 'Access-Control-Allow-Origin' '*' always;
        add_header 'Access-Control-Allow-Methods' 'GET, POST, PUT, DELETE, OPTIONS, PATCH' always;
        add_header 'Access-Control-Allow-Headers' 'Content-Type, Authorization, X-Requested-With, X-Signature, Accept, Origin' always;
        add_header 'Access-Control-Allow-Credentials' 'true' always;
        add_header 'Access-Control-Max-Age' '86400' always;
        add_header 'Content-Length' '0' always;
        add_header 'Content-Type' 'text/plain; charset=utf-8' always;
        return 204;
    }

    # Payment and subscription endpoints - route to payment service (port 8081)
    location ~ ^/api/(checkout|tier|subscription|cancel-subscription|webhook) {
        # Proxy to payment service
        proxy_pass http://localhost:8081;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Origin \$http_origin;
        
        # Pass through all headers including webhook signature
        proxy_set_header X-Signature \$http_x_signature;
        proxy_set_header Authorization \$http_authorization;
        
        # Increase timeouts for payment processing
        proxy_connect_timeout       60s;
        proxy_send_timeout          60s;
        proxy_read_timeout          60s;
        
        # Disable buffering for real-time responses
        proxy_buffering off;
    }

    # Health check endpoint for payment service
    location /health {
        proxy_pass http://localhost:8081;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    # Gateway service endpoints (port 8080)
    location /v1/ {
        proxy_pass http://localhost:8080;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Origin \$http_origin;
        proxy_set_header Authorization \$http_authorization;
        
        # Standard timeouts
        proxy_connect_timeout       30s;
        proxy_send_timeout          30s;
        proxy_read_timeout          30s;
    }

    # Classifier service endpoints (port 8082)
    location /classify/ {
        proxy_pass http://localhost:8082;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Origin \$http_origin;
        proxy_set_header Authorization \$http_authorization;
        
        # Standard timeouts
        proxy_connect_timeout       30s;
        proxy_send_timeout          30s;
        proxy_read_timeout          30s;
    }

    # Default route - gateway service
    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Origin \$http_origin;
        proxy_set_header Authorization \$http_authorization;
        
        # Standard timeouts
        proxy_connect_timeout       30s;
        proxy_send_timeout          30s;
        proxy_read_timeout          30s;
    }

    # Error pages
    error_page 404 /404.html;
    error_page 500 502 503 504 /50x.html;
    location = /50x.html {
        root /var/www/html;
    }
}
EOF

    # Enable the site
    echo "🔗 Enabling nginx site..."
    sudo ln -sf $CONFIG_FILE $ENABLED_DIR/dev-backend
    
elif [[ "$OSTYPE" == "darwin"* ]]; then
    sudo tee $CONFIG_FILE > /dev/null <<EOF
server {
    listen 80;
    server_name localhost;

    # Enable CORS for all origins (development only)
    add_header 'Access-Control-Allow-Origin' '*' always;
    add_header 'Access-Control-Allow-Methods' 'GET, POST, PUT, DELETE, OPTIONS, PATCH' always;
    add_header 'Access-Control-Allow-Headers' 'Content-Type, Authorization, X-Requested-With, X-Signature, Accept, Origin' always;
    add_header 'Access-Control-Allow-Credentials' 'true' always;
    add_header 'Access-Control-Max-Age' '86400' always;

    # Handle OPTIONS requests for CORS preflight
    if (\$request_method = 'OPTIONS') {
        add_header 'Access-Control-Allow-Origin' '*' always;
        add_header 'Access-Control-Allow-Methods' 'GET, POST, PUT, DELETE, OPTIONS, PATCH' always;
        add_header 'Access-Control-Allow-Headers' 'Content-Type, Authorization, X-Requested-With, X-Signature, Accept, Origin' always;
        add_header 'Access-Control-Allow-Credentials' 'true' always;
        add_header 'Access-Control-Max-Age' '86400' always;
        add_header 'Content-Length' '0' always;
        add_header 'Content-Type' 'text/plain; charset=utf-8' always;
        return 204;
    }

    # Payment and subscription endpoints - route to payment service (port 8081)
    location ~ ^/api/(checkout|tier|subscription|cancel-subscription|webhook) {
        # Proxy to payment service
        proxy_pass http://localhost:8081;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Origin \$http_origin;
        
        # Pass through all headers including webhook signature
        proxy_set_header X-Signature \$http_x_signature;
        proxy_set_header Authorization \$http_authorization;
        
        # Increase timeouts for payment processing
        proxy_connect_timeout       60s;
        proxy_send_timeout          60s;
        proxy_read_timeout          60s;
        
        # Disable buffering for real-time responses
        proxy_buffering off;
    }

    # Health check endpoint for payment service
    location /health {
        proxy_pass http://localhost:8081;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    # Gateway service endpoints (port 8080)
    location /v1/ {
        proxy_pass http://localhost:8080;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Origin \$http_origin;
        proxy_set_header Authorization \$http_authorization;
        
        # Standard timeouts
        proxy_connect_timeout       30s;
        proxy_send_timeout          30s;
        proxy_read_timeout          30s;
    }

    # Classifier service endpoints (port 8082)
    location /classify/ {
        proxy_pass http://localhost:8082;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Origin \$http_origin;
        proxy_set_header Authorization \$http_authorization;
        
        # Standard timeouts
        proxy_connect_timeout       30s;
        proxy_send_timeout          30s;
        proxy_read_timeout          30s;
    }

    # Default route - gateway service
    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Origin \$http_origin;
        proxy_set_header Authorization \$http_authorization;
        
        # Standard timeouts
        proxy_connect_timeout       30s;
        proxy_send_timeout          30s;
        proxy_read_timeout          30s;
    }

    # Error pages
    error_page 404 /404.html;
    error_page 500 502 503 504 /50x.html;
}
EOF
fi

# 6. Test nginx configuration
echo "🔍 Testing nginx configuration..."
sudo nginx -t

# 7. Start nginx
echo "🚀 Starting nginx..."
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    sudo systemctl start nginx
    sudo systemctl enable nginx
elif [[ "$OSTYPE" == "darwin"* ]]; then
    sudo nginx
fi

echo ""
echo "🎉 Development nginx setup completed successfully!"
echo ""
echo "📊 Configuration Summary:"
echo "  ✅ Domain: http://localhost"
echo "  ✅ Port: 80"
echo "  ✅ CORS: Allow ALL origins (including localhost:3000)"
echo "  ✅ Methods: GET, POST, PUT, DELETE, OPTIONS, PATCH"
echo "  ✅ Headers: Content-Type, Authorization, X-Signature, etc."
echo ""
echo "🔧 Service Routing:"
echo "  📍 Payment endpoints → localhost:8081"
echo "    • /api/checkout"
echo "    • /api/tier"
echo "    • /api/subscription"
echo "    • /api/cancel-subscription"
echo "    • /api/webhook"
echo "  📍 Health check → localhost:8081"
echo "    • /health"
echo "  📍 Gateway endpoints → localhost:8080"
echo "    • /v1/*"
echo "    • / (default)"
echo "  📍 Classifier endpoints → localhost:8082"
echo "    • /classify/*"
echo ""
echo "🌐 Your frontend at localhost:3000 can now make requests to:"
echo "  • http://localhost/api/checkout"
echo "  • http://localhost/api/subscription"
echo "  • http://localhost/v1/profile"
echo "  • http://localhost/health"
echo "  • And all other backend endpoints"
echo ""
echo "⚠️  Note: This configuration is for DEVELOPMENT only!"
echo "   It allows requests from ALL origins - do not use in production!"
echo "" 