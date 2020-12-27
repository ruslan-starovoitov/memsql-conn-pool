FROM memsql/cluster-in-a-box
ENV LICENSE_KEY={LICENSE_KEY}
ENV START_AFTER_INIT=Y
COPY ./init.sql /docker-entrypoint-initdb.d/
