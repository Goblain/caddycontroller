FROM scratch

ADD caddy /caddy
ADD caddycontroller /caddycontroller

CMD [ "/caddycontroller" ]

