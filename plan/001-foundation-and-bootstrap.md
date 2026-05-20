# 001: Foundation And Bootstrap

## Summary

Create the repository, local development model, management-plane bootstrap, and engineering conventions required to build Servicer as a real control plane.

## Why This Exists

The platform needs a stable base before API and UX work begin. This phase establishes the management cluster, code structure, delivery conventions, and local developer path.

## Scope

- define repo layout for frontend, API, controllers, charts, and generated artifacts
- establish local development and integration environments
- bootstrap the management-plane baseline components
- choose the initial identity, secret, and internal Git strategy
- install Argo CD for platform delivery responsibilities
- define code generation, testing, and CI conventions

## Recommended Outputs

- repository conventions document
- local bootstrap scripts or dev environment definitions
- management cluster bootstrap manifests
- internal Git and generated delivery repo strategy
- initial Argo CD install and application model
- API and controller skeletons

## Acceptance Criteria

- a developer can stand up the management-plane baseline locally
- the project has a clear code layout for frontend, API, and controllers
- CRD code generation and basic controller scaffolding run in CI
- Argo CD can sync baseline platform components from the chosen source path
- identity and secret dependencies are documented well enough for the next phase

## Notes

Keep this phase narrow. It should make the next tickets easy to execute, not attempt to deliver catalog functionality early.