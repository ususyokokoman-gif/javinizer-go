<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import FormToggle from '$lib/components/settings/FormToggle.svelte';
	import FormPasswordInput from '$lib/components/settings/FormPasswordInput.svelte';
	import FormTextInput from '$lib/components/settings/FormTextInput.svelte';
	import type { CatalogIDValidationConfig, SettingsConfig } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
		inputClass: string;
	}

	let { config, inputClass }: Props = $props();
	const enabled = $derived(config.metadata.catalog_id_validation?.enabled ?? false);

	function ensureConfig(): CatalogIDValidationConfig {
		if (!config.metadata.catalog_id_validation) {
			config.metadata.catalog_id_validation = {
				enabled: false,
				threshold: 0.8,
				model: 'jev-latest',
				endpoint: 'https://api.typesafe.ai/v1/systemone',
				api_key: ''
			};
		}
		return config.metadata.catalog_id_validation;
	}

	function setThreshold(event: Event): void {
		const value = Number.parseFloat((event.currentTarget as HTMLInputElement).value);
		if (!Number.isNaN(value)) ensureConfig().threshold = value;
	}
</script>

<SettingsSection
	title={m.settings_catalog_id_validation_title()}
	description={m.settings_catalog_id_validation_desc()}
	defaultExpanded={false}
>
	<FormToggle
		label={m.settings_catalog_id_validation_enable_label()}
		description={m.settings_catalog_id_validation_enable_desc()}
		checked={config.metadata.catalog_id_validation?.enabled ?? false}
		onchange={(value) => {
			ensureConfig().enabled = value;
		}}
	/>

	<fieldset disabled={!enabled} class="space-y-0" class:opacity-60={!enabled}>
		<div class="py-4 border-b border-border">
			<label class="block text-sm font-medium mb-2" for="catalog-id-jev-threshold">
				{m.settings_catalog_id_validation_threshold_label()}
			</label>
			<input
				id="catalog-id-jev-threshold"
				type="number"
				min="0.01"
				max="1"
				step="0.01"
				value={config.metadata.catalog_id_validation?.threshold ?? 0.8}
				oninput={setThreshold}
				class="w-32 px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all bg-background text-sm"
			/>
			<p class="text-sm text-muted-foreground mt-1">
				{m.settings_catalog_id_validation_threshold_desc()}
			</p>
		</div>

		<FormTextInput
			label={m.settings_catalog_id_validation_model_label()}
			description={m.settings_catalog_id_validation_model_desc()}
			value={config.metadata.catalog_id_validation?.model ?? 'jev-latest'}
			placeholder="jev-latest"
			onchange={(value) => {
				ensureConfig().model = value.trim();
			}}
		/>

		<FormTextInput
			label={m.settings_catalog_id_validation_endpoint_label()}
			description={m.settings_catalog_id_validation_endpoint_desc()}
			value={config.metadata.catalog_id_validation?.endpoint ?? 'https://api.typesafe.ai/v1/systemone'}
			placeholder="https://api.typesafe.ai/v1/systemone"
			onchange={(value) => {
				ensureConfig().endpoint = value.trim();
			}}
		/>

		<FormPasswordInput
			label={m.settings_catalog_id_validation_api_key_label()}
			description={m.settings_catalog_id_validation_api_key_desc()}
			value={config.metadata.catalog_id_validation?.api_key ?? ''}
			onchange={(value) => {
				ensureConfig().api_key = value;
			}}
		/>

		<div class="py-4 text-sm text-muted-foreground space-y-1">
			<p>{m.settings_catalog_id_validation_policy_note()}</p>
			<p>{m.settings_catalog_id_validation_env_note()}</p>
		</div>
	</fieldset>
</SettingsSection>
