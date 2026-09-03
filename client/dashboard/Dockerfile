FROM nginxinc/nginx-unprivileged:alpine

# Copy the build output to nginx
COPY dist /usr/share/nginx/html
COPY --chown=nginx:nginx dist/index.html /usr/share/nginx/html/index.html

# nginx.conf is an envsubst template: the image entrypoint renders it to
# /etc/nginx/conf.d/default.conf at startup, substituting ${GRAM_CDN_URL}.
# A later entrypoint script substitutes ${GRAM_ADMIN_SERVER_URL} in index.html.
COPY nginx.conf /etc/nginx/templates/default.conf.template
COPY --chmod=755 40-admin-server-url.sh /docker-entrypoint.d/

# Defaults: no CDN rewrite or admin link. Both must always be defined — envsubst
# only substitutes variables present in the environment, and unsubstituted
# values would be parsed by nginx as undefined nginx variables at startup.
ENV GRAM_CDN_URL=""
ENV GRAM_ADMIN_SERVER_URL=""

EXPOSE 3000

CMD ["nginx", "-g", "daemon off;"]