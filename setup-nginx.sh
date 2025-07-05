#!/bin/bash

# Nginx Setup Script for api.mayura.rocks
# This script generates SSL certificates and configures nginx

set -e

echo "Setting up nginx for api.mayura.rocks..."

# Create SSL directories
sudo mkdir -p /etc/ssl/certs
sudo mkdir -p /etc/ssl/private

# Generate SSL certificate (self-signed for local development)
# For production, replace this with your actual SSL certificate
echo "Generating SSL certificate..."
sudo openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
    -keyout /etc/ssl/private/api.mayura.rocks.key \
    -out /etc/ssl/certs/api.mayura.rocks.crt \
    -subj "/C=US/ST=State/L=City/O=Organization/OU=OrgUnit/CN=api.mayura.rocks"

# Set proper permissions
sudo chmod 600 /etc/ssl/private/api.mayura.rocks.key
sudo chmod 644 /etc/ssl/certs/api.mayura.rocks.crt

# Copy rate limiting configuration
echo "Installing rate limiting configuration..."
sudo cp nginx-rate-limit.conf /etc/nginx/conf.d/

# Copy nginx configuration
echo "Installing nginx configuration..."
sudo cp nginx.conf /etc/nginx/sites-available/api.mayura.rocks

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

echo "Setup complete!"
echo "Your API is now available at https://api.mayura.rocks"
echo ""
echo "Note: This uses a self-signed certificate. For production, replace with:"
echo "- Let's Encrypt certificate using certbot"
echo "- Commercial SSL certificate from your provider"
echo ""
echo "To use Let's Encrypt instead, run:"
echo "sudo certbot --nginx -d api.mayura.rocks" 