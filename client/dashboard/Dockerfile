FROM nginxinc/nginx-unprivileged:alpine

# Copy the build output to nginx
COPY dist /usr/share/nginx/html

# nginx.conf is an envsubst template: the image entrypoint renders it to
# /etc/nginx/conf.d/default.conf at startup, substituting ${GRAM_CDN_URL} and
# ${GRAM_ADMIN_SERVER_URL}.
COPY nginx.conf /etc/nginx/templates/default.conf.template

# Defaults: no CDN rewrite or admin link. Both must always be defined — envsubst
# only substitutes variables present in the environment, and unsubstituted
# values would be parsed by nginx as undefined nginx variables at startup.
ENV GRAM_CDN_URL=""
ENV GRAM_ADMIN_SERVER_URL=""

EXPOSE 3000

CMD ["nginx", "-g", "daemon off;"]