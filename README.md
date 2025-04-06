# traefik-lambdarequesttransformer
This plugin parses the HTTP request and converts it to a Lambda request 2.0 event object with requestContext.authorizer.lambda.    Only supports lambda running in local environment. For local development, this plugin is required to be used in conjunction with httplambdaauth plugin.
