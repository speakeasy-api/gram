# Single-origin dashboard image: prebuilt SPA dist + an nginx that reverse-
# proxies Gram API paths to gram-server. Build context = a temp staging dir
# (see build-and-load.sh / the manual build below) containing: dist/,
# dashboard-nginx.conf, gram_proxy.conf.
FROM nginxinc/nginx-unprivileged:alpine
COPY dist /usr/share/nginx/html
COPY dashboard-nginx.conf /etc/nginx/conf.d/default.conf
COPY gram_proxy.conf /etc/nginx/gram_proxy.conf
EXPOSE 3000
CMD ["nginx", "-g", "daemon off;"]
