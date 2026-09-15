FROM nginxinc/nginx-unprivileged:alpine

# Copy the build output to nginx
COPY dist /usr/share/nginx/html

# Copied straight into conf.d rather than into /etc/nginx/templates: this app
# needs no envsubst pass, because it serves its assets same-origin (no CDN
# rewrite) alongside the Gram admin API.
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 3000

CMD ["nginx", "-g", "daemon off;"]
