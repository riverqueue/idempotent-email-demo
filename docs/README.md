# idempotent-email-demo [![Build Status](https://github.com/riverqueue/idempotent-email-demo/actions/workflows/ci.yaml/badge.svg?branch=master)](https://github.com/riverqueue/idempotent-email-demo/actions)

A small demo showing how River could be used to build an API for idempotently sending email.

## Setup

Requires the use of [Direnv](https://direnv.net/):

    cp .envrc.sample .envrc
    direnv allow

## Run demo

    createdb river_dev
    go run github.com/riverqueue/river/cmd/river@latest migrate-up --database-url "$DATABASE_URL"
    go run main.go

## Run tests

    createdb river_test
    go run github.com/riverqueue/river/cmd/river@latest migrate-up --database-url "$TEST_DATABASE_URL"
