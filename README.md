# govd
a telegram bot for downloading media from various platforms.

this project draws significant inspiration from [yt-dlp](https://github.com/yt-dlp/yt-dlp).

- official instance: [@govd_bot](https://t.me/govd_bot)
- support group: [govdsupport](https://t.me/govdsupport)

---

* [dependencies](#dependencies)
* [installation](#installation)
    * [build](#build)
    * [docker](#docker-recommended)
* [configuration](#configuration)
* [authentication](#authentication)
* [proxying](#proxying)
* [todo](#todo)

# dependencies
* ffmpeg >= 7.x
    * with shared libraries
* libheif >= 1.19.7
* pkg-config
* sql database
    * mysql or mariadb 

# installation
## build
> [!NOTE]
> there's no official support for windows yet. if you want to run the bot on it, please follow [docker installation](#docker-recommended).

1. clone the repository:
    ```bash
    git clone https://github.com/govdbot/govd.git && cd govd
    ```

2. edit the `.env` file to set the database properties.  
   for enhanced security, it is recommended to change the `DB_PASSWORD` property in the `.env` file.

3. make sure your database is up and running.

4. build and run the bot:

    ```bash
    sh build.sh && ./govd
    ```

## docker (recommended)
1. create or update the `.env` file to ensure the database properties match the environment variables defined for the mariadb service in the `docker-compose.yml` file.  
   for enhanced security, it is recommended to change the `MARIADB_PASSWORD` property in `docker-compose.yaml` and ensure `DB_PASSWORD` in `.env` matches it.

    the following line in the `.env` file **must** be set as:

    ```
    DB_HOST=db
    ``` 

2. build the image using the dockerfile:

    ```bash
    docker build -t govd-bot .
    ```

3. run the compose to start all services:

    ```bash
    docker compose up -d
    ```

> [!TIP]
> after updating your `.env` file, rebuilding the image is not necessary. simply restart the containers with the `docker compose restart` command to apply the changes.


# configuration
you can configure the bot using the `.env` file. here are the available options:

## database

| variable     | description       | default   |
|--------------|-------------------|-----------|
| db_host      | database host     | localhost |
| db_port      | database port     | 3306      |
| db_name      | database name     | govd      |
| db_user      | database user     | govd      |
| db_password  | database password | password  |

## telegram

| variable           | description                      | default                              |
|--------------------|----------------------------------|--------------------------------------|
| bot_api_url        | telegram bot api url             | https://api.telegram.org             |
| bot_token          | telegram bot token               | 12345678:abc-def1234ghikl-zyx57w2p0s |
| concurrent_updates | max concurrent updates handled   | 50                                   |

## downloads

| variable       | description                    | default    |
|----------------|--------------------------------|------------|
| downloads_dir  | directory for downloaded files | downloads  |

## proxying

| variable     | description                 | default            |
|--------------|-----------------------------|--------------------|
| http_proxy   | http proxy (optional)       | none _(disabled)_  |
| https_proxy  | https proxy (optional)      | none _(disabled)_  |
| no_proxy     | no proxy domains (optional) | none _(disabled)_  |

## misc

| variable       | description                               | default                             |
|----------------|-------------------------------------------|-------------------------------------|
| repo_url       | project repository url                    | https://github.com/govdbot/govd     |
| profiler_port  | port for profiler http server (pprof)     | 0 _(disabled)_                      |
| log_level      | log level (debug, info, warn, error)      | info                                |
| log_file       | whether to enable file logging            | false                               |
| max_duration   | max duration in seconds                   | 3600                                |
| max_file_size  | max file size in mb                       | 1000                                |

## extractors
you can configure specific extractors options with `ext-cfg.yaml` file ([learn more](CONFIGURATION.md)).

> [!IMPORTANT]  
> to avoid limits on files, you should host your own telegram botapi and set `BOT_API_URL` variable according. public bot instance is currently running under a botapi fork, [tdlight-telegram-bot-api](https://github.com/tdlight-team/tdlight-telegram-bot-api), but you can use the official botapi client too.

# proxying
there are two types of proxying available:
* **http proxy**: this is a standard http proxy that can be used to route requests through a proxy server. you can set the `HTTP_PROXY` and `HTTPS_PROXY` environment variables to use this feature. (SOCKS5 is supported too)
* **edge proxy**: this is a custom proxy that is used to route requests through a specific url. currenrly, you can only set this proxy with `ext-cfg.yaml` file ([learn more](EDGEPROXY.md)).

> [!TIP]
> by settings `NO_PROXY` environment variable, you can specify domains that should not be proxied.

# authentication
some extractors require cookies to access the content. please refer to [this page](AUTHENTICATION.md) for more information on how to set up authentication for each extractor.

# todo
* [ ] add tests
* [ ] add support for telegram webhooks
* [ ] switch to pgsql (maybe)
* [ ] better api
* [ ] better docs
